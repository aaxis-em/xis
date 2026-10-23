# Tests install.ps1 against a local server that serves a release the way
# GitHub does. Runs with PowerShell 7 on Windows, and on Linux and macOS too:
# there it checks everything but the PATH change and the run of xis.exe.
# XIS_TEST_SHELL picks the PowerShell that runs install.ps1, for example
# powershell for Windows PowerShell 5.1.
$ErrorActionPreference = 'Stop'
$shell = 'pwsh'
if ($env:XIS_TEST_SHELL) { $shell = $env:XIS_TEST_SHELL }

$root = Split-Path -Parent $PSScriptRoot
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("xis-ps-test-" + [Guid]::NewGuid())
$version = '0.0.1'
$archive = "xis_${version}_windows_amd64.zip"
$release = Join-Path $tmp "srv/download/v$version"
New-Item -ItemType Directory -Path $release, (Join-Path $tmp 'stage') -Force | Out-Null

$py = 'python3'
if (-not (Get-Command $py -ErrorAction SilentlyContinue)) { $py = 'python' }
$server = $null
try {
	$env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
	Push-Location $root
	go build -ldflags "-X github.com/aaxis-em/xis/internal/buildinfo.Version=$version" -o (Join-Path $tmp 'stage/xis.exe') ./cmd/xis
	if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
	Pop-Location
	Remove-Item Env:GOOS, Env:GOARCH
	Copy-Item (Join-Path $root 'README.md') (Join-Path $tmp 'stage')
	Compress-Archive -Path (Join-Path $tmp 'stage/*') -DestinationPath (Join-Path $release $archive)
	$hash = (Get-FileHash -Algorithm SHA256 (Join-Path $release $archive)).Hash.ToLower()
	Set-Content -Path (Join-Path $release 'checksums.txt') -Value "$hash  $archive"

	$serverPy = Join-Path $tmp 'server.py'
	Set-Content -Path $serverPy -Value @'
import http.server, sys
root, version = sys.argv[1], sys.argv[2]
class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=root, **kw)
    def do_HEAD(self):
        if self.path == "/latest":
            self.send_response(302); self.send_header("Location", "/tag/v" + version); self.end_headers()
        elif self.path.startswith("/tag/"):
            self.send_response(200); self.end_headers()
        else:
            super().do_HEAD()
    def log_message(self, *a):
        pass
s = http.server.HTTPServer(("127.0.0.1", 0), H)
print(s.server_address[1], flush=True)
s.serve_forever()
'@
	$portFile = Join-Path $tmp 'port'
	$server = Start-Process -FilePath $py -ArgumentList $serverPy, (Join-Path $tmp 'srv'), $version -RedirectStandardOutput $portFile -PassThru -NoNewWindow
	for ($i = 0; $i -lt 50; $i++) {
		if ((Test-Path $portFile) -and (Get-Content $portFile)) { break }
		Start-Sleep -Milliseconds 100
	}
	$base = "http://127.0.0.1:$((Get-Content $portFile | Select-Object -First 1).Trim())"

	function Install($dir, $ver, $noPath = '1') {
		$env:XIS_BASE_URL = $base; $env:XIS_INSTALL_DIR = $dir; $env:XIS_ARCH = 'amd64'; $env:XIS_NO_PATH = $noPath
		$env:XIS_VERSION = $ver
		$out = & $shell -NoLogo -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root 'install.ps1') 2>&1
		return @{ Ok = ($LASTEXITCODE -eq 0); Out = ($out -join "`n") }
	}
	function Check($name, $cond, $detail) {
		if ($cond) { Write-Host "ok   $name" } else { Write-Host "FAIL $name`n$detail"; throw "test failed: $name" }
	}

	$r = Install (Join-Path $tmp 'bin1') ''
	Check 'latest release' ($r.Ok -and (Test-Path (Join-Path $tmp 'bin1/xis.exe'))) $r.Out
	if ($IsWindows) {
		$v = & (Join-Path $tmp 'bin1/xis.exe') version | Select-Object -First 1
		Check 'installed binary runs' ($v -match "xis $version") $v
	}
	$r = Install (Join-Path $tmp 'bin2') $version
	Check 'pinned version without v' ($r.Ok -and (Test-Path (Join-Path $tmp 'bin2/xis.exe'))) $r.Out

	Set-Content -Path (Join-Path $release 'checksums.txt') -Value "deadbeef  $archive"
	$r = Install (Join-Path $tmp 'bin3') ''
	Check 'checksum mismatch fails' ((-not $r.Ok) -and ($r.Out -match 'checksum mismatch')) $r.Out
	Check 'nothing installed on mismatch' (-not (Test-Path (Join-Path $tmp 'bin3/xis.exe'))) ''
	$r = Install (Join-Path $tmp 'bin4') 'v9.9.9'
	Check 'unknown version fails' (-not $r.Ok) $r.Out

	if ($IsWindows) {
		Set-Content -Path (Join-Path $release 'checksums.txt') -Value "$hash  $archive"
		$key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
		$opt = [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
		$saved = $key.GetValue('Path', $null, $opt)
		$savedKind = if ($null -ne $saved) { $key.GetValueKind('Path') } else { $null }
		try {
			$key.SetValue('Path', '%XIS_TEST_HOME%\bin;C:\existing', [Microsoft.Win32.RegistryValueKind]::ExpandString)
			$dir = Join-Path $tmp 'bin5'
			$r = Install $dir '' '0'
			$after = $key.GetValue('Path', '', $opt)
			Check 'PATH keeps %VAR% entries' ($r.Ok -and $after -eq "%XIS_TEST_HOME%\bin;C:\existing;$dir") "$after`n$($r.Out)"
			Check 'PATH stays expandable' ($key.GetValueKind('Path') -eq [Microsoft.Win32.RegistryValueKind]::ExpandString) ''
			$r = Install $dir '' '0'
			Check 'second install adds nothing' ($key.GetValue('Path', '', $opt) -eq $after) ($key.GetValue('Path', '', $opt))
		} finally {
			if ($null -ne $saved) { $key.SetValue('Path', $saved, $savedKind) } else { $key.DeleteValue('Path', $false) }
			$key.Close()
		}
	}
} finally {
	if ($server) { Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue }
	Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

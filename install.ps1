# Install xis on Windows from its GitHub releases:
#
#   irm https://raw.githubusercontent.com/aaxis-em/xis/main/install.ps1 | iex
#
# Environment variables:
#   XIS_VERSION      a release such as v0.1.0 (default: latest)
#   XIS_INSTALL_DIR  where to put xis.exe (default: %LOCALAPPDATA%\Programs\xis)
#   XIS_NO_PATH      set to 1 to leave the user PATH as it is
#
# The script checks the SHA-256 of the download against checksums.txt of the
# release and stops on any mismatch. It runs in Windows PowerShell 5.1 and in
# PowerShell 7.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

function Fail($msg) { throw "xis install: $msg" }

function Get-FinalUrl($url) {
	$r = Invoke-WebRequest -Uri $url -Method Head -UseBasicParsing
	if ($r.BaseResponse.ResponseUri) { return $r.BaseResponse.ResponseUri.AbsoluteUri }
	return $r.BaseResponse.RequestMessage.RequestUri.AbsoluteUri
}

$repo = 'aaxis-em/xis'
$base = "https://github.com/$repo/releases"
if ($env:XIS_BASE_URL) { $base = $env:XIS_BASE_URL }
$version = 'latest'
if ($env:XIS_VERSION) { $version = $env:XIS_VERSION }
if ($env:XIS_INSTALL_DIR) { $dir = $env:XIS_INSTALL_DIR } else { $dir = Join-Path $env:LOCALAPPDATA 'Programs\xis' }

$cpu = $env:PROCESSOR_ARCHITECTURE
if ($env:XIS_ARCH) { $cpu = $env:XIS_ARCH }
switch ($cpu) {
	{ $_ -in 'AMD64', 'amd64', 'x64' } { $arch = 'amd64' }
	{ $_ -in 'ARM64', 'arm64' } { $arch = 'arm64' }
	default { Fail "no release for the CPU $cpu" }
}

if ($version -eq 'latest') {
	$version = (Get-FinalUrl "$base/latest") -replace '.*/tag/', ''
	if ($version -notmatch '^v\d') { Fail "no published release found at $base" }
}
if ($version -notmatch '^v') { $version = "v$version" }

$archive = "xis_$($version.Substring(1))_windows_$arch.zip"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("xis-install-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
	Write-Host "Downloading xis $version for windows/$arch"
	try {
		Invoke-WebRequest -Uri "$base/download/$version/$archive" -OutFile (Join-Path $tmp $archive) -UseBasicParsing
		Invoke-WebRequest -Uri "$base/download/$version/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing
	} catch {
		Fail "download of $archive failed: $($_.Exception.Message)"
	}

	$want = ''
	foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
		$f = $line -split '\s+'
		if ($f.Count -ge 2 -and $f[1] -eq $archive) { $want = $f[0].ToLower() }
	}
	if (-not $want) { Fail "checksums.txt has no entry for $archive" }
	$got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $archive)).Hash.ToLower()
	if ($got -ne $want) { Fail "checksum mismatch for $archive (got $got, want $want)" }

	Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'x') -Force
	New-Item -ItemType Directory -Path $dir -Force | Out-Null
	Copy-Item -Path (Join-Path $tmp 'x\xis.exe') -Destination (Join-Path $dir 'xis.exe') -Force
} finally {
	Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host "Installed xis $version to $(Join-Path $dir 'xis.exe')"
if ($env:XIS_NO_PATH -ne '1') {
	$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
	if (-not $userPath) { $userPath = '' }
	if (-not (($userPath -split ';') -contains $dir)) {
		[Environment]::SetEnvironmentVariable('Path', (($userPath.TrimEnd(';') + ';' + $dir).TrimStart(';')), 'User')
		$env:Path = "$env:Path;$dir"
		Write-Host "Added $dir to your user PATH. Open a new terminal to use xis."
	}
}
Write-Host "Next: run 'xis init' once, then 'xis hook install --agent claude-code' in each repository."

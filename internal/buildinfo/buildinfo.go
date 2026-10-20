// Package buildinfo holds version data that the linker sets at build time.
package buildinfo

import "runtime/debug"

// The release build sets these values with -ldflags "-X".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info returns the version, commit and build date. When the linker did not
// set a value, Info reads it from the Go build information if possible.
func Info() (version, commit, date string) {
	version, commit, date = Version, Commit, Date
	if bi, ok := debug.ReadBuildInfo(); ok {
		if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.time":
				if date == "" {
					date = s.Value
				}
			}
		}
	}
	if commit == "" {
		commit = "unknown"
	}
	if date == "" {
		date = "unknown"
	}
	return version, commit, date
}

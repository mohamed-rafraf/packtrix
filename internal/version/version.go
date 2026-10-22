// Package version reports how the running packtrix binary was built.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set by release builds:
//
//	-ldflags "-X github.com/packtrix/packtrix/internal/version.version=v0.1.0
//	          -X github.com/packtrix/packtrix/internal/version.commit=<sha>
//	          -X github.com/packtrix/packtrix/internal/version.date=<RFC 3339>"
var (
	version string
	commit  string
	date    string
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Date      string `json:"date,omitempty"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns the build information. Values not set through ldflags fall
// back to what the Go toolchain embeds: the module version and the VCS
// revision and time.
func Get() Info {
	info := Info{
		Version:   version,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" {
			info.Version = bi.Main.Version
		}
		var revision, modified string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				modified = s.Value
			}
		}
		if info.Commit == "" && revision != "" {
			info.Commit = revision
			if modified == "true" {
				info.Commit += "-dirty"
			}
		}
	}
	if info.Version == "" {
		info.Version = "(devel)"
	}
	return info
}

func (i Info) String() string {
	return fmt.Sprintf("Version:    %s\nCommit:     %s\nBuilt:      %s\nGo version: %s\nPlatform:   %s",
		i.Version, orUnknown(i.Commit), orUnknown(i.Date), i.GoVersion, i.Platform)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

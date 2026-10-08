package sysinfo

import (
	"cmp"
	"runtime/debug"
	"strings"
)

const (
	// develVersion is the version of a binary without a module version.
	develVersion = "(devel)"
	// revisionLen is the number of revision characters in the version, as
	// in a Go pseudo-version.
	revisionLen = 12
	// goDirty is the suffix that the go command adds to the module version
	// of a build with local changes.
	goDirty = "+dirty"
)

// ToolVersion returns the version of this binary, for example
// "v1.2.0" or "(devel)+3f2c9a1b7d4e-dirty".
func ToolVersion() string { return versionFrom(debug.ReadBuildInfo()) }

// versionFrom returns the module version, or a development version with the
// short VCS revision when no module version is available. Local changes use
// "-dirty" rather than the go command's "+dirty" suffix.
func versionFrom(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return develVersion
	}
	version := strings.TrimSuffix(cmp.Or(info.Main.Version, develVersion), goDirty)
	if rev := shortRevision(info); version == develVersion && rev != "" {
		version += "+" + rev
	}
	if setting(info, "vcs.modified") == "true" {
		version += "-dirty"
	}
	return version
}

// shortRevision returns the first characters of the VCS revision, or "".
func shortRevision(info *debug.BuildInfo) string {
	rev := setting(info, "vcs.revision")
	return rev[:min(len(rev), revisionLen)]
}

// setting returns the build setting key, or "".
func setting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

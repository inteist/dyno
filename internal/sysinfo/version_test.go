package sysinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

// buildInfo returns build info with version and the VCS settings kv.
func buildInfo(version string, kv ...string) *debug.BuildInfo {
	info := &debug.BuildInfo{Main: debug.Module{Path: "dyno", Version: version}}
	for i := 0; i+1 < len(kv); i += 2 {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return info
}

func TestVersionFrom(t *testing.T) {
	t.Parallel()
	const rev = "3f2c9a1b7d4e5f60718293a4b5c6d7e8f9012345"
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"no build info", nil, false, "(devel)"},
		{"nil build info", nil, true, "(devel)"},
		{"release without VCS", buildInfo("v1.2.0"), true, "v1.2.0"},
		{"empty version", buildInfo(""), true, "(devel)"},
		{"devel without VCS", buildInfo("(devel)"), true, "(devel)"},
		{"devel with revision", buildInfo("(devel)", "vcs.revision", rev, "vcs.modified", "false"), true, "(devel)+3f2c9a1b7d4e"},
		{"devel with changes", buildInfo("(devel)", "vcs.revision", rev, "vcs.modified", "true"), true, "(devel)+3f2c9a1b7d4e-dirty"},
		{"short revision", buildInfo("(devel)", "vcs.revision", "abc123"), true, "(devel)+abc123"},
		{"tag with revision", buildInfo("v1.2.0", "vcs.revision", rev), true, "v1.2.0"},
		{"tag with changes", buildInfo("v1.2.0+dirty", "vcs.revision", rev, "vcs.modified", "true"), true, "v1.2.0-dirty"},
		{"pseudo-version", buildInfo("v0.0.0-20261005140212-3f2c9a1b7d4e", "vcs.revision", rev, "vcs.modified", "false"), true,
			"v0.0.0-20261005140212-3f2c9a1b7d4e"},
		{"pseudo-version with changes", buildInfo("v0.0.0-20261005140212-3f2c9a1b7d4e+dirty", "vcs.revision", rev, "vcs.modified", "true"), true,
			"v0.0.0-20261005140212-3f2c9a1b7d4e-dirty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := versionFrom(tt.info, tt.ok); got != tt.want {
				t.Errorf("versionFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolVersion(t *testing.T) {
	t.Parallel()
	if got := ToolVersion(); got == "" || strings.ContainsAny(got, " \n") {
		t.Errorf("ToolVersion() = %q, want one word", got)
	}
}

package scripts_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestInstallerPinsByteOrientedSystemAwk(t *testing.T) {
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "awk"), []byte("#!/bin/sh\necho incompatible-PATH-awk >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := agentDryRun(t, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"), "LC_ALL=C", "DYNO_TAG=café")
	if err != nil || !strings.Contains(string(out), "<string>café</string>") {
		t.Fatalf("installer did not pin system awk: %s %v", out, err)
	}
}

func TestScheduledUnicodeLabelsIndependentOfLocale(t *testing.T) {
	for _, locale := range []string{"C", "POSIX", "en_US.UTF-8", ""} {
		for _, tag := range []string{"café", "kv–q8", "日本語", `literal\nbackslash`} {
			out, err := agentDryRun(t, "LC_ALL="+locale, "LANG=", "DYNO_TAG="+tag)
			if err != nil || !strings.Contains(string(out), "<string>"+tag+"</string>") {
				t.Errorf("locale %s tag %q: %s %v", locale, tag, out, err)
			}
		}
		for _, value := range []string{strings.Repeat("日", 30), strings.Repeat("é", 64)} {
			out, err := agentDryRun(t, "LC_ALL="+locale, "DYNO_BACKEND=openai", "DYNO_MODELS=one", "DYNO_SERVER_VERSION="+value, "DYNO_MODEL_REVISION="+value)
			if err != nil {
				t.Errorf("locale %s valid labels: %s %v", locale, out, err)
			}
		}
		for _, bad := range []string{"\u200b", "\u202e", "\u2028", "\u2029", "\ufeff", "\ue000", "\U000f0001", "\U00100001", string([]byte{0xff})} {
			if out, err := agentDryRun(t, "LC_ALL="+locale, "DYNO_TAG=bad"+bad); err == nil {
				t.Errorf("locale %s accepted unsafe %q: %s", locale, bad, out)
			}
		}
	}
}

func TestScheduledLabelBoundaries(t *testing.T) {
	for _, variable := range []string{"DYNO_SERVER_VERSION", "DYNO_MODEL_REVISION"} {
		for _, bad := range []string{" padded", "padded ", "bad\x1b", "bad\u200b", strings.Repeat("日", 65)} {
			if out, err := agentDryRun(t, "LC_ALL=C", "DYNO_BACKEND=openai", "DYNO_MODELS=one", variable+"="+bad); err == nil {
				t.Errorf("accepted %s=%q: %s", variable, bad, out)
			}
		}
	}
	for _, bad := range []string{"sha256:short", "sha256:" + strings.Repeat("z", 64), "sha256:" + strings.Repeat("a", 63), "sha256:" + strings.Repeat("a", 65)} {
		if out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=one", "DYNO_MODEL_REVISION="+bad); err == nil {
			t.Errorf("accepted malformed digest %q: %s", bad, out)
		}
	}
	if out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=one", "DYNO_MODEL_REVISION=sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatalf("valid digest: %s %v", out, err)
	}
	for _, bad := range []string{"a/../../x", "a label", "a\nlabel"} {
		if out, err := agentDryRun(t, "DYNO_LABEL="+bad); err == nil {
			t.Errorf("accepted unsafe launchd label %q: %s", bad, out)
		}
	}
}

func TestScheduledDurationBoundariesIndependentOfLocale(t *testing.T) {
	for _, locale := range []string{"C", "en_US.UTF-8"} {
		for _, duration := range []string{"0", "+0", "1.5s", ".5s", "1500µs", "1.5ms", "2562047.78801521550222222222h", "1µs", "1μs", "1us", "2562047h47m16.854775807s", "9223372036854775807ns", "9223372036.854775807s", "9223372037s", "2562047h47m16.854775808s", "9223372036854775808ns", "9223372036854775807ns1ns", "999999999999999999999999h", "-1ns", "abc"} {
			_, wantErr := time.ParseDuration(duration)
			if strings.HasPrefix(duration, "-") {
				wantErr = fmt.Errorf("negative interval")
			}
			out, err := agentDryRun(t, "LC_ALL="+locale, "DYNO_INTERVAL="+duration)
			if (err != nil) != (wantErr != nil) {
				t.Errorf("locale %s duration %s: %s %v; Go %v", locale, duration, out, err, wantErr)
			}
		}
	}
}

// A toolchain Unicode update must regenerate the checked-in installer table.
func installerPrintableRanges(t *testing.T) [][2]int {
	t.Helper()
	f, err := os.Open("printable-ranges.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ranges [][2]int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		var r [2]int
		if _, err := fmt.Sscan(line, &r[0], &r[1]); err != nil {
			t.Fatal(err)
		}
		ranges = append(ranges, r)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return ranges
}

func TestInstallerPrintableRangesMatchGo(t *testing.T) {
	ranges := installerPrintableRanges(t)
	i := 0
	for cp := 0; cp <= unicode.MaxRune; cp++ {
		for i < len(ranges) && ranges[i][1] < cp {
			i++
		}
		present := i < len(ranges) && ranges[i][0] <= cp
		if present != unicode.IsPrint(rune(cp)) {
			t.Fatalf("U+%04X disagrees with Go; regenerate with go run scripts/generate-printable-ranges.go", cp)
		}
	}
}

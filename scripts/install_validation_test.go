package scripts_test

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduledRejectsSettingsTheBinaryCannotUse(t *testing.T) {
	for _, settings := range [][]string{{"DYNO_MODELS=,,,,"}, {"DYNO_INTERVAL=abc"}, {"DYNO_INTERVAL=+"}, {"DYNO_INTERVAL=2562048h"}, {"DYNO_INTERVAL=1x"}, {"DYNO_TAG=bad space"}, {"DYNO_TAG=bad　space"}, {"DYNO_LABEL=../bad"}, {"DYNO_SERVER_VERSION=" + strings.Repeat("a", 65)}, {"DYNO_MODELS=bad\x1bmodel"}, {"DYNO_CACHE=bad"}, {"DYNO_CACHE=miss,miss"}, {"DYNO_BACKEND=openai", "DYNO_MODELS=one,two", "DYNO_MODEL_REVISION=rev"}, {"DYNO_BACKEND=openai", "DYNO_MODELS=one", "DYNO_CACHE=prefix"}, {"DYNO_BACKEND=openai", "DYNO_MODELS=one", "DYNO_MODEL_REVISION=unsafe\x1b[31m"}} {
		if out, err := agentDryRun(t, settings...); err == nil {
			t.Errorf("accepted %v: %s", settings, out)
		}
	}
}

func TestScheduledEscapesXMLWithoutRewritingInsertedPlaceholders(t *testing.T) {
	endpoint := "https://host/proxy/<literal>&token={{MODELS}}/v1"
	out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=served", "DYNO_BASE_URL="+endpoint, "DYNO_TAG={{MODELS}}")
	if err != nil {
		t.Fatalf("render: %v: %s", err, out)
	}
	decoder := xml.NewDecoder(bytes.NewReader(out))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "string" {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, endpoint) || !strings.Contains(joined, "\n{{MODELS}}\n") {
		t.Fatal("XML values were not preserved", joined)
	}
}

func TestUninstallAndDryRunAreRejectedWithoutSideEffects(t *testing.T) {
	home, shim := t.TempDir(), t.TempDir()
	label := "dev.dyno.test.dryrun"
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(shim, "calls")
	if err := os.WriteFile(filepath.Join(shim, "launchctl"), []byte("#!/bin/sh\nprintf called > \"$DYNO_TEST_LOG\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "./install-agent.sh", "--uninstall", "--dry-run")
	cmd.Env = []string{"HOME=" + home, "PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH"), "DYNO_LABEL=" + label, "DYNO_TEST_LOG=" + log, "DYNO_BACKEND=bad"}
	if out, err := cmd.CombinedOutput(); err == nil || !bytes.Contains(out, []byte("use only one")) {
		t.Fatalf("ambiguous uninstall flags accepted: %v %s", err, out)
	}
	if got, err := os.ReadFile(plist); err != nil || string(got) != "keep" {
		t.Fatal("dry-run removed job file", err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("dry-run invoked launchctl", err)
	}
}

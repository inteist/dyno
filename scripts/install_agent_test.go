package scripts_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func agentDryRun(t *testing.T, extra ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("bash", "./install-agent.sh", "--dry-run")
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "DYNO_") && !strings.HasPrefix(item, "OPENAI_") && !strings.HasPrefix(item, "HOME=") && !strings.HasPrefix(item, "XDG_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	home, shim := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "launchctl"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"), fmt.Sprintf("DYNO_LABEL=dev.dyno.test.%x", sha256.Sum256([]byte(home))), "DYNO_BIN=/usr/bin/true", "OPENAI_API_KEY=never-embed-this-credential")
	cmd.Env = append(cmd.Env, extra...)
	return cmd.CombinedOutput()
}

func TestScheduledProviderArgumentsWithoutCredentials(t *testing.T) {
	t.Parallel()
	for _, external := range []bool{false, true} {
		var extra []string
		if external {
			extra = []string{"DYNO_BACKEND=openai", "DYNO_BASE_URL=https://gpu.example/llm/v1", "DYNO_MODELS=team/id", "DYNO_API_KEY_ENV=BENCH_KEY", "BENCH_KEY=never-embed-this-credential"}
		}
		out, err := agentDryRun(t, extra...)
		if err != nil {
			t.Fatalf("render: %v: %s", err, out)
		}
		if bytes.Contains(out, []byte("never-embed-this-credential")) || bytes.Contains(out, []byte("{{PROVIDER_ARGS}}")) {
			t.Fatal("credential or unresolved placeholder in plist")
		}
		decoder := xml.NewDecoder(bytes.NewReader(out))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid XML: %v", err)
			}
		}
		if external {
			if !bytes.Contains(out, []byte("<string>openai</string>")) || !bytes.Contains(out, []byte("https://gpu.example/llm/v1")) || !bytes.Contains(out, []byte("<string>BENCH_KEY</string>")) {
				t.Fatal("compatible arguments missing")
			}
		} else if bytes.Contains(out, []byte("<string>--backend</string>")) {
			t.Fatal("native default arguments changed")
		}
	}
}

func TestScheduledExplicitModelsAndMalformedEndpoints(t *testing.T) {
	t.Parallel()
	if out, err := agentDryRun(t, "DYNO_BACKEND=openai"); err == nil || !bytes.Contains(out, []byte("DYNO_MODELS")) {
		t.Fatalf("missing models accepted: %s %v", out, err)
	}
	for _, raw := range []string{"http://host:abc/v1", "http://host:0/v1", "http://host:65536/v1", "http://host:/v1", "http://[bad::address]/v1", "http://[:::]/v1", "http://host/v1\t", "http://host/v1#fragment", "http://host/%ZZ/v1", "http://host/%0/v1", "http://host/%00/v1", "http://host/%7F/v1", "http://host/%C2%89/v1"} {
		if out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=served-id", "DYNO_BASE_URL="+raw); err == nil {
			t.Errorf("accepted malformed %q: %s", raw, out)
		}
	}
	for _, raw := range []string{"http://host:8000/v1", "http://[::1]:8000/v1", "https://host/proxy/api/v1", "http://[::ffff:127.0.0.1]/v1", "HTTP://LOCALHOST:000080/v1", "http://host/model%2Fname/v1"} {
		if out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=served-id", "DYNO_BASE_URL="+raw); err != nil {
			t.Errorf("rejected valid %q: %s %v", raw, out, err)
		}
	}
}

func TestScheduledAllCompatibleSettings(t *testing.T) {
	t.Parallel()
	out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=served-id", "DYNO_SERVER_KIND=vllm", "DYNO_SERVER_VERSION=0.31.0", "DYNO_MODEL_REVISION=weights-rev", "DYNO_USAGE=off", "DYNO_TOKEN_LIMIT_FIELD=max_completion_tokens", "DYNO_SKIP_MODEL_DISCOVERY=1", "DYNO_OMIT_SEED=1", "DYNO_OMIT_TEMPERATURE=1")
	if err != nil {
		t.Fatalf("render: %s %v", out, err)
	}
	for _, flag := range []string{"--server", "--server-version", "--model-revision", "--usage", "--token-limit-field", "--skip-model-discovery", "--omit-seed", "--omit-temperature"} {
		if !bytes.Contains(out, []byte("<string>"+flag+"</string>")) {
			t.Errorf("missing %s", flag)
		}
	}
	if bytes.Contains(out, []byte("--api-key-env")) {
		t.Fatal("optional default credential became an explicit required variable")
	}
}

func TestUninstallIgnoresStaleProviderSettings(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	label := fmt.Sprintf("dev.dyno.test.%x", sha256.Sum256([]byte(home)))
	shim := t.TempDir()
	log := filepath.Join(shim, "calls")
	if err := os.WriteFile(filepath.Join(shim, "launchctl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DYNO_TEST_LAUNCHCTL_LOG\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "./install-agent.sh", "--uninstall")
	cmd.Env = []string{"PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + home, "DYNO_LABEL=" + label, "DYNO_TEST_LAUNCHCTL_LOG=" + log, "DYNO_BACKEND=invalid", "DYNO_BASE_URL=invalid", "DYNO_HOUR=invalid"}
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("removed "+label)) {
		t.Fatalf("uninstall blocked: %s %v", out, err)
	}
	calls, err := os.ReadFile(log)
	if err != nil || !bytes.Contains(calls, []byte("/"+label)) || bytes.Contains(calls, []byte("dev.dyno.nightly")) {
		t.Fatalf("unsafe launchctl call: %s %v", calls, err)
	}
}

func TestScheduledEndpointRejectsCredentialURLs(t *testing.T) {
	t.Parallel()
	for _, url := range []string{"https://user:never-embed-this-credential@gpu.example/v1", "http://gpu.example/v1?key=never-embed-this-credential"} {
		out, err := agentDryRun(t, "DYNO_BACKEND=openai", "DYNO_MODELS=served-id", "DYNO_BASE_URL="+url)
		if err == nil {
			t.Fatal("credential-bearing URL accepted")
		}
		if bytes.Contains(out, []byte("never-embed-this-credential")) {
			t.Fatal("credential leaked in installer error")
		}
	}
}

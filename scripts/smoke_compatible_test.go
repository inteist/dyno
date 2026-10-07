package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSmokeRequiresStatisticallyValidRuns(t *testing.T) {
	t.Parallel()
	binary := filepath.Join(t.TempDir(), "fixture-dyno")
	body := "#!/usr/bin/env bash\nif [[ \"$1\" == report ]]; then echo \"dyno report · 1 run · ${DYNO_FAKE_VALID} valid · 1 session\"; else printf '%s\\n' \"$@\" > \"$DYNO_FAKE_ARGS\"; fi\n"
	if err := os.WriteFile(binary, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args")
	for _, count := range []string{"0", "1"} {
		cmd := exec.Command("bash", "./smoke-compatible.sh")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DYNO_BIN=" + binary, "DYNO_FAKE_VALID=" + count, "DYNO_FAKE_ARGS=" + argsFile, "DYNO_SERVER_KIND=vllm", "DYNO_LIVE_OPENAI=1", "OPENAI_BASE_URL=http://127.0.0.1:8000/v1", "DYNO_MODEL=alias"}
		out, err := cmd.CombinedOutput()
		args, readErr := os.ReadFile(argsFile)
		if readErr != nil || !strings.Contains(string(args), "--server\nvllm\n") || strings.Contains(string(args), "--server-kind") {
			t.Fatalf("smoke emitted incorrect server flag: %s %v", args, readErr)
		}
		if count == "0" && (err == nil || !strings.Contains(string(out), "no valid benchmark runs")) {
			t.Fatalf("invalid smoke passed: %s %v", out, err)
		}
		if count == "1" && err != nil {
			t.Fatalf("valid smoke failed: %s %v", out, err)
		}
	}
}

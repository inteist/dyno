package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestServerFlagPreservesProviderMetadata(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"run", "models"} {
		for _, server := range []string{"unknown", "vllm"} {
			for _, beforeBackend := range []bool{false, true} {
				args := []string{command}
				if command == "run" {
					args = append(args, "--models", "served")
				}
				if beforeBackend {
					args = append(args, "--server", server, "--backend", "openai")
				} else {
					args = append(args, "--backend", "openai", "--server", server)
				}
				args = append(args, "--server-version", "0.31.0")
				got, stderr, err := parse(testEnv(t, nil), args...)
				if err != nil {
					t.Fatalf("%v: %v (%s)", args, err, stderr)
				}
				var common Common
				switch c := got.(type) {
				case RunCommand:
					common = c.Common
				case ModelsCommand:
					common = c.Common
				default:
					t.Fatalf("unexpected command %T", got)
				}
				if common.ServerKind != server || common.ServerVersion != "0.31.0" {
					t.Fatalf("provider metadata changed: %+v", common)
				}
			}
		}
		args := []string{command, "--backend", "openai"}
		if command == "run" {
			args = append(args, "--models", "served")
		}
		for _, flags := range [][]string{{"--server", "unsupported"}, {"--server-kind", "vllm"}} {
			got, _, err := parse(testEnv(t, nil), append(append([]string{}, args...), flags...)...)
			if got != nil || !errors.Is(err, ErrUsage) {
				t.Fatalf("invalid %v accepted: %T %v", flags, got, err)
			}
			if flags[0] == "--server" && !strings.Contains(err.Error(), "unknown --server") {
				t.Fatalf("stale server diagnostic: %v", err)
			}
		}
		native := []string{command, "--server", "vllm"}
		if command == "run" {
			native = append(native, "--models", "native")
		}
		_, _, err := parse(testEnv(t, nil), native...)
		if !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "--server requires --backend openai") {
			t.Fatalf("native validation bypassed: %v", err)
		}
	}
}

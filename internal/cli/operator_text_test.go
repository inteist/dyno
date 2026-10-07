package cli

import (
	"io"
	"strings"
	"testing"
)

func TestEndpointErrorIdentifiesEnvironmentSource(t *testing.T) {
	_, err := Parse([]string{"run", "--backend", "openai", "--models", "served", "--db", "/unused.db"}, Env{Getenv: func(name string) string {
		if name == "OPENAI_BASE_URL" {
			return "http://host/v1?secret=bad"
		}
		return ""
	}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "OPENAI_BASE_URL:") {
		t.Fatal("environment endpoint was mislabeled", err)
	}
}

func TestServerVersionRejectsPadding(t *testing.T) {
	for _, value := range []string{" 0.31", "0.31 ", " 0.31 "} {
		_, err := Parse([]string{"run", "--backend", "openai", "--models", "served", "--db", "/unused.db", "--server-version", value}, Env{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "--server-version must be unpadded") {
			t.Errorf("accepted padded version %q: %v", value, err)
		}
	}
}

func TestOperatorLabelsRejectTerminalControlsAndOverlongText(t *testing.T) {
	for _, flag := range []string{"--model-revision", "--server-version", "--tag"} {
		for _, value := range []string{"rev\x1b[31m", "rev\nnext", "rev\ttext", "rev\u200b", strings.Repeat("界", 65)} {
			if flag == "--tag" && value == strings.Repeat("界", 65) {
				continue
			}
			_, err := Parse([]string{"run", "--backend", "openai", "--models", "served", flag, value}, Env{Getenv: func(name string) string {
				if name == "DYNO_DB" {
					return "/unused.db"
				}
				return ""
			}}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "printable text") {
				t.Errorf("accepted %s %q: %v", flag, value, err)
			}
		}
	}
	for _, key := range []string{"1", "v", "v1", "/"} {
		if _, err := Parse([]string{"run", "--backend", "openai", "--base-url", "http://localhost/v1", "--models", "served", "--model-revision", strings.Repeat("a", 40)}, Env{Getenv: func(name string) string {
			if name == "DYNO_DB" {
				return "/unused.db"
			}
			if name == "OPENAI_API_KEY" {
				return key
			}
			return ""
		}}, io.Discard); err != nil {
			t.Errorf("short key rejected valid route: %q: %v", key, err)
		}
	}
}

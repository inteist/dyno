package app

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"dyno/internal/cli"
)

func TestEndpointAuthenticationNoticeAndPlainHTTPWarningAreSecretSafe(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"http://gpu.example/path/v1", "https://gpu.example/v1", "http://localhost:8000/v1", "http://127.0.0.1:8000/v1", "http://[::1]:8000/v1"} {
		for _, key := range []string{"", "do-not-print-credential"} {
			command, err := cli.Parse([]string{"models", "--backend", "openai", "--base-url", endpoint}, cli.Env{Getenv: func(string) string { return key }}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			warnEndpoint(command.(cli.ModelsCommand).Common, IO{Err: &output})
			want := key != "" && strings.Contains(endpoint, "://gpu.example/")
			if (output.Len() > 0) != want {
				t.Errorf("endpoint=%s authenticated=%v warning=%q", endpoint, key != "", output.String())
			}
			wantHTTP := want && strings.HasPrefix(endpoint, "http://")
			if strings.Contains(output.String(), "plain HTTP") != wantHTTP {
				t.Errorf("plain HTTP warning mismatch: %q", output.String())
			}
			if want && !strings.Contains(output.String(), "OPENAI_API_KEY") {
				t.Fatal("credential variable notice missing")
			}
			if strings.Contains(output.String(), "do-not-print-credential") || strings.Contains(output.String(), endpoint) {
				t.Fatal("sensitive endpoint or credential in diagnostic")
			}
		}
	}
}

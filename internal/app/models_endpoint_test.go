package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"dyno/internal/cli"
	"dyno/internal/openaicompat"
)

func TestModelsUsesCompatibleDiscoveryWithoutLifecycleOrRenaming(t *testing.T) {
	t.Parallel()
	const id = "team/CaseSensitive-ID"
	const key = "model-list-key-do-not-print"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/proxy/v1/models" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model","created":1,"owned_by":"fixture"}]}`, id)
	}))
	defer server.Close()
	cmd, err := cli.Parse([]string{"models", "--backend", "openai", "--base-url", server.URL + "/proxy/v1", "--api-key-env", "BENCH_KEY"}, cli.Env{Getenv: func(name string) string {
		if name == "BENCH_KEY" {
			return key
		}
		return ""
	}}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Execute(context.Background(), cmd, IO{Out: &out, Err: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || !strings.Contains(out.String(), id) || strings.Contains(out.String(), id+":latest") || strings.Contains(out.String(), key) {
		t.Fatalf("incorrect models output: %s; requests=%d", out.String(), requests.Load())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, id) {
			fields := strings.Fields(line)
			if len(fields) != 8 {
				t.Fatalf("unknown metadata lacks placeholders: %q", line)
			}
			for _, field := range fields[1:] {
				if field != "-" {
					t.Errorf("invented compatible metadata: %q", line)
				}
			}
		}
	}
}

func TestModelsReturnsDiscoveryRouteError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"models route unavailable"}}`)
	}))
	defer server.Close()
	cmd, err := cli.Parse([]string{"models", "--backend", "openai", "--base-url", server.URL + "/v1"}, cli.Env{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	err = Execute(context.Background(), cmd, IO{Out: io.Discard, Err: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "list models") || !errors.Is(err, openaicompat.ErrDiscoveryUnsupported) {
		t.Fatalf("discovery failure lost: %v", err)
	}
}

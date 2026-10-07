package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dyno/internal/cli"
	"dyno/internal/report"
)

func TestTrustedEndpointRevisionSeparatesActualGenerationPolicies(t *testing.T) {
	t.Parallel()
	var seeded, unseeded atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if _, ok := payload["seed"]; ok {
			seeded.Add(1)
		} else {
			unseeded.Add(1)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	db := filepath.Join(t.TempDir(), "policy.db")
	for i := range 2 {
		args := []string{"run", "--backend", "openai", "--base-url", server.URL + "/v1", "--models", "served", "--model-revision", "immutable-revision", "--db", db, "--runs", "1", "--interval", "0", "--num-predict", "2", "--plain"}
		if i == 1 {
			args = append(args, "--omit-seed")
		}
		command, err := cli.Parse(args, cli.Env{}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := Execute(context.Background(), command, IO{Out: &output, Err: io.Discard}); err != nil {
			t.Fatalf("session: %v\n%s", err, output.String())
		}
		if strings.Contains(output.String(), "no runs match") {
			t.Fatal("history omitted current policy")
		}
	}
	rows := storedRuns(t, db)
	if len(rows) != 2 || seeded.Load() != 2 || unseeded.Load() != 2 {
		t.Fatalf("rows=%d seeded=%d unseeded=%d", len(rows), seeded.Load(), unseeded.Load())
	}
	if rows[0].Model.StorageKey() != rows[1].Model.StorageKey() {
		t.Fatal("trusted revision identity changed with inference policy")
	}
	for _, dimensions := range [][]report.Dimension{report.DefaultDimensions(), allTimeDimensions(), {report.ByModel, report.ByMode}} {
		built := report.Build(rows, dimensions, time.UTC)
		if built.Valid != 2 || len(built.Groups) != 2 || built.Groups[0].Key.Policy == built.Groups[1].Key.Policy {
			t.Fatalf("merged effective policies for %v: %+v", dimensions, built.Groups)
		}
	}
}

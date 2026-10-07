package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dyno/internal/cli"
	"dyno/internal/domain"
	"dyno/internal/report"
)

// Exercise CLI, production factory/SDK, runner, recorder and summary together.
func TestCompatibleEndpointSessionsPreserveUnknownMetricsAndOpaqueIDs(t *testing.T) {
	t.Parallel()
	const model = "team/CaseSensitive-ID"
	const credential = "fixture-credential-do-not-persist"
	var chats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			t.Errorf("authentication missing")
		}
		switch r.URL.Path {
		case "/proxy/v1/models":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model","created":1,"owned_by":"fixture"}]}`, model)
		case "/proxy/v1/chat/completions":
			chats.Add(1)
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("request: %v", err)
				w.WriteHeader(400)
				return
			}
			if request["model"] != model || request["stream"] != true {
				t.Errorf("opaque model/stream changed: %v", request)
			}
			for _, field := range []string{"num_ctx", "keep_alive"} {
				if _, ok := request[field]; ok {
					t.Errorf("native option %s sent", field)
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"chat-fixture\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n", model)
			io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n")
			io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n")
			io.WriteString(w, "data: [DONE]\n\n")
		default:
			t.Errorf("unexpected external lifecycle/admin request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	db := filepath.Join(t.TempDir(), "endpoint.db")
	for i := range 2 {
		endpoint := server.URL + "/proxy/v1"
		if i == 0 {
			endpoint += "/" // Canonical storage must still match all-time history.
		}
		args := []string{"run", "--backend", "openai", "--base-url", endpoint, "--models", model, "--db", db, "--runs", "1", "--interval", "0", "--num-predict", "2", "--plain", "--server", "vllm"}
		command, err := cli.Parse(args, cli.Env{Getenv: func(key string) string {
			if key == "OPENAI_API_KEY" {
				return credential
			}
			return ""
		}}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := Execute(context.Background(), command, IO{Out: &output, Err: io.Discard}); err != nil {
			t.Fatalf("session: %v\n%s", err, output.String())
		}
		if strings.Contains(output.String(), "no runs match") {
			t.Fatal("current endpoint disappeared from its all-time summary")
		}
		if strings.Contains(output.String(), credential) {
			t.Fatal("credential in UI")
		}
		if !strings.Contains(output.String(), model) || strings.Contains(output.String(), model+":latest") {
			t.Fatalf("model renamed in output: %s", output.String())
		}
	}
	rows := storedRuns(t, db)
	if len(rows) != 2 || chats.Load() != 4 {
		t.Fatalf("rows=%d chats=%d: expected one warmup and one measured request per session", len(rows), chats.Load())
	}
	for _, row := range rows {
		metrics := row.Measurement.Server
		if row.Backend != "openai" || row.ServerKind != "vllm" || row.Model.Name != model || row.Model.Digest != "" {
			t.Fatalf("fabricated deployment/model facts: %+v", row)
		}
		if !metrics.Has(domain.MetricEvalTokens) || metrics.EvalTokens != 2 || metrics.Has(domain.MetricCachedTokens) || metrics.Has(domain.MetricEvalDuration) || metrics.Has(domain.MetricPromptDuration) {
			t.Fatalf("unknown metrics became zero: %+v", metrics)
		}
	}
	if rows[0].Model.StorageKey() == rows[1].Model.StorageKey() {
		t.Fatal("unverified alias merged across sessions")
	}
	built := report.Build(rows, report.DefaultDimensions(), time.UTC)
	if built.Valid != 2 || len(built.Groups) != 2 {
		t.Fatalf("compatible results lost or merged: valid=%d groups=%d", built.Valid, len(built.Groups))
	}
	data := readFile(t, db)
	if strings.Contains(data, credential) {
		t.Fatal("credential in database")
	}
}

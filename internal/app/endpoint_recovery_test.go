package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"dyno/internal/cli"
)

func TestCompatibleRequestInterruptionDoesNotSkipRemainingRuns(t *testing.T) {
	for _, tc := range []struct{ reset, allFail bool }{{false, false}, {true, false}, {true, true}} {
		reset := tc.reset
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			var chats atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/models") {
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, strings.Repeat("<html>unavailable</html>", 10000))
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				count := chats.Add(1)
				if count == 3 || tc.allFail && count > 1 { // warm-up succeeds
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					if reset {
						_ = conn.(*net.TCPConn).SetLinger(0)
					}
					_ = conn.Close()
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			db := filepath.Join(t.TempDir(), "recovery.db")
			command, err := cli.Parse([]string{"run", "--backend", "openai", "--base-url", server.URL + "/v1", "--models", "served", "--runs", "4", "--interval", "0", "--num-predict", "2", "--db", db, "--plain"}, cli.Env{Getenv: func(string) string { return "" }}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := Execute(context.Background(), command, IO{Out: &output, Err: io.Discard}); err == nil || (!tc.allFail && err.Error() != "1 of 4 steps failed") || (tc.allFail && !strings.Contains(err.Error(), "3 consecutive failed steps")) {
				t.Fatalf("session aborted: %v\n%s", err, output.String())
			}
			rows := storedRuns(t, db)
			if tc.allFail {
				if len(rows) != 3 || chats.Load() != 4 {
					t.Fatalf("unbounded failures: rows=%d chats=%d", len(rows), chats.Load())
				}
				return
			}
			if len(rows) != 4 || chats.Load() != 5 {
				t.Fatalf("remaining runs skipped or retry added: rows=%d chats=%d", len(rows), chats.Load())
			}
			failures := 0
			for _, row := range rows {
				if row.Err != "" {
					failures++
				}
			}
			if failures != 1 || rows[1].Err == "" || rows[2].Err != "" || rows[3].Err != "" {
				t.Fatalf("incorrect run recovery: %+v", rows)
			}
		})
	}
}

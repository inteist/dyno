package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamRejectsSDKStringCoercion(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"content", "role"} {
		for _, literal := range []string{"12", "true", "{}", "[]"} {
			t.Run(field+"/"+literal, func(t *testing.T) {
				t.Parallel()
				payload := frame(fmt.Sprintf(`{"choices":[{"index":0,"delta":{%q:%s}}]}`, field, literal)) + frame(finishChunk) + frame(usageChunk) + frame("[DONE]")
				server := streamServer(t, payload)
				client := newTestClient(t, Config{BaseURL: server.URL})
				progress := 0
				_, err := client.Chat(context.Background(), request(), func(int) { progress++ })
				if !errors.Is(err, ErrProtocol) || progress != 0 {
					t.Fatalf("fabricated streamed text: err=%v progress=%d", err, progress)
				}
			})
		}
	}
}

func TestStreamAllowsAbsentNullAndEmptyText(t *testing.T) {
	t.Parallel()
	for _, delta := range []string{`{}`, `{"content":null,"role":null}`, `{"content":""}`} {
		t.Run(delta, func(t *testing.T) {
			t.Parallel()
			server := streamServer(t, frame(`{"choices":[{"index":0,"delta":`+delta+`}]}`)+frame(finishChunk)+frame(usageChunk)+frame("[DONE]"))
			client := newTestClient(t, Config{BaseURL: server.URL})
			result, err := client.Chat(context.Background(), request(), nil)
			if err != nil || result.Reply.Content != "" || result.Measurement.Client.TimeToFirstToken != 0 {
				t.Fatalf("null/absent text mishandled: %+v %v", result, err)
			}
		})
	}
}

func TestDiscoveryRequiresLiteralStringIDs(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"123", "true", "{}", "[]", "null", `""`} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"object":"list","data":[{"id":%s,"object":"model"}]}`, literal)
			}))
			defer server.Close()
			client := newTestClient(t, Config{BaseURL: server.URL})
			models, err := client.Models(context.Background())
			if !errors.Is(err, ErrProtocol) || len(models) != 0 {
				t.Fatalf("invented model ID: %+v err=%v", models, err)
			}
		})
	}
}

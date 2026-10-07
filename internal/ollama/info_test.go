package ollama

import (
	"context"
	"testing"

	"dyno/internal/domain"
)

func TestNativeInfoAndNamingDoNotRequireServer(t *testing.T) {
	t.Parallel()
	client, err := New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(context.Background())
	if err != nil || info.Backend != domain.BackendOllama || info.MeasurementProtocol != domain.ProtocolOllama || !domain.IsNativePolicy(info.ContextPolicy, info.ConfigJSON) {
		t.Fatalf("native metadata incompatible with historical policy: %+v %v", info, err)
	}
	for _, pair := range [][2]string{{"llama", "llama:latest"}, {"team/model:tag", "team/model:tag"}, {"localhost:5000/team/model", "localhost:5000/team/model:latest"}} {
		if got := client.NormalizeModelName(pair[0]); got != pair[1] {
			t.Errorf("normalize %q=%q want %q", pair[0], got, pair[1])
		}
	}
}

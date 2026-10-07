package cli

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"dyno/internal/domain"
)

func TestEndpointDefaultsAndOpaqueNames(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"run", "--backend", "openai", "--models", "team/model,alias:tag"},
		{"run", "--models", "team/model,alias:tag", "--backend", "openai"},
	} {
		cmd, stderr, err := parse(testEnv(t, nil), args...)
		if err != nil {
			t.Fatalf("parse: %v (%s)", err, stderr)
		}
		c := cmd.(RunCommand)
		if !reflect.DeepEqual(c.Options.Models, []string{"team/model", "alias:tag"}) {
			t.Fatalf("opaque IDs changed: %v", c.Options.Models)
		}
		if !c.Options.External || c.Options.Generation.ContextSize != 0 || c.Options.KeepAlive != 0 {
			t.Fatalf("native defaults leaked: %+v", c.Options)
		}
		if c.BaseURL != "http://127.0.0.1:8000/v1" || c.Usage != "required" || c.TokenLimitField != "max_tokens" {
			t.Fatalf("unexpected defaults: %+v", c.Common)
		}
	}
}

func TestEndpointEnvironmentAndSecrets(t *testing.T) {
	t.Parallel()
	env := testEnv(t, map[string]string{
		"OPENAI_BASE_URL": "https://proxy.example/llm/api",
		"OPENAI_API_KEY":  "do-not-use", "BENCH_KEY": "unique-secret",
		"OLLAMA_HOST": "http://wrong.example",
	})
	cmd, _, err := parse(env, "run", "--backend", "openai", "--api-key-env", "BENCH_KEY", "--models", "alias", "--usage", "off", "--skip-model-discovery", "--omit-seed", "--omit-temperature")
	if err != nil {
		t.Fatal(err)
	}
	c := cmd.(RunCommand)
	if c.APIKey() != "unique-secret" || c.BaseURL != "https://proxy.example/llm/api" || !c.SkipModelDiscovery || !c.OmitSeed || !c.OmitTemperature {
		t.Fatal("explicit endpoint environment was not resolved")
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "unique-secret") || strings.Contains(string(b), "do-not-use") {
		t.Fatal("credential serialized")
	}
	native, _, err := parse(env, "run", "--models", "a")
	if err != nil {
		t.Fatal(err)
	}
	n := native.(RunCommand)
	if n.BaseURL != "" || n.APIKey() != "" || n.BackendName() != "ollama" {
		t.Fatal("endpoint environment contaminated Ollama")
	}
}

func TestEndpointHostAlias(t *testing.T) {
	t.Parallel()
	cmd, _, err := parse(testEnv(t, nil), "run", "--host", "https://proxy.example/api", "--models", "id", "--backend", "openai")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.(RunCommand).BaseURL != "https://proxy.example/api" {
		t.Fatal("host path was not preserved")
	}
}

func TestEndpointInvalidOptions(t *testing.T) {
	t.Parallel()
	for _, extra := range [][]string{
		{"--backend", "other"},
		{"--backend", "openai", "--host", "http://a/v1", "--base-url", "http://b/v1"},
		{"--backend", "openai", "--base-url", "https://user:secret@example/v1"},
		{"--backend", "openai", "--base-url", "http://example/v1?key=secret"},
		{"--backend", "openai", "--base-url", "http://example/v1?"},
		{"--backend", "openai", "--base-url", "http://example/v1#x"},
		{"--backend", "openai", "--num-ctx", "8192"},
		{"--backend", "openai", "--keep-alive", "0s"},
		{"--backend", "openai", "--cache", "continue"},
		{"--backend", "openai", "--cache", "prefix"},
		{"--backend", "openai", "--usage", "guess"},
		{"--backend", "openai", "--token-limit-field", "num_predict"},
		{"--base-url", "http://example/v1"},
		{"--usage", "off"},
	} {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			args := append([]string{"run", "--models", "alias"}, extra...)
			cmd, _, err := parse(testEnv(t, nil), args...)
			if cmd != nil || !errors.Is(err, ErrUsage) {
				t.Fatalf("got %T, %v", cmd, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("secret leaked in configuration error: %v", err)
			}
		})
	}
}

func TestExplicitCredentialVariableMustExist(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"BENCH_KEY", "", "BAD-NAME", "7BAD"} {
		_, _, err := parse(testEnv(t, nil), "models", "--backend", "openai", "--api-key-env", name)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("empty/unset/invalid variable %q: %v", name, err)
		}
	}
	_, _, err := parse(testEnv(t, nil), "models", "--backend", "openai")
	if err != nil {
		t.Fatalf("optional default key became mandatory: %v", err)
	}
}

func TestOmittedGenerationCannotContradictExplicitOptions(t *testing.T) {
	t.Parallel()
	for _, pair := range [][]string{{"--omit-seed", "--seed", "7"}, {"--omit-temperature", "--temperature", "1.5"}} {
		args := append([]string{"run", "--backend", "openai", "--models", "alias"}, pair...)
		_, _, err := parse(testEnv(t, nil), args...)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("contradiction accepted %v: %v", pair, err)
		}
	}
	cmd, _, err := parse(testEnv(t, nil), "run", "--backend", "openai", "--models", "alias", "--omit-seed", "--omit-temperature")
	if err != nil {
		t.Fatal(err)
	}
	if g := cmd.(RunCommand).Options.Generation; g.Seed != 0 || g.Temperature != 0 {
		t.Fatalf("unused defaults affect workload: %+v", g)
	}
}

func TestCanonicalEndpointsAndInvalidPortsAreUsageErrors(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"http://host:0/v1", "http://host:65536/v1", "http://host:abc/v1", "http://host:/v1", "http://host/%00/v1"} {
		_, _, err := parse(testEnv(t, nil), "models", "--backend", "openai", "--base-url", base)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("bad API base %q: %v", base, err)
		}
	}
	cmd, _, err := parse(testEnv(t, nil), "models", "--backend", "openai", "--host", "http://LOCALHOST:80/v1/", "--base-url", "http://localhost/v1")
	if err != nil || cmd.(ModelsCommand).BaseURL != "http://localhost/v1" {
		t.Fatalf("equivalent bases conflict: %v %v", cmd, err)
	}
	_, _, err = parse(testEnv(t, map[string]string{"OPENAI_API_KEY": "secret-key"}), "models", "--backend", "openai", "--base-url", "https://host/secret-key/v1")
	if !errors.Is(err, ErrUsage) || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("credential in path accepted/exposed: %v", err)
	}
}

func TestCredentialPathRejectsEscapedAndDecodedSecrets(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"section/key", "section%2Fkey"} {
		_, _, err := parse(testEnv(t, map[string]string{"OPENAI_API_KEY": key}), "models", "--backend", "openai", "--base-url", "https://host/section%2Fkey/v1")
		if !errors.Is(err, ErrUsage) || strings.Contains(err.Error(), key) {
			t.Fatalf("credential-bearing path accepted/exposed: %v", err)
		}
	}
}

func TestEndpointRevisionSingleModel(t *testing.T) {
	t.Parallel()
	_, _, err := parse(testEnv(t, nil), "run", "--backend", "openai", "--models", "a,b", "--model-revision", "immutable")
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("got %v", err)
	}
}

func TestReportKeepsOpaqueIDsAndOfflineBackendFilter(t *testing.T) {
	t.Parallel()
	cmd, _, err := parse(testEnv(t, map[string]string{"OPENAI_BASE_URL": "not a URL"}), "report", "--backend", "openai", "--model", "alias,team/model", "--cache", "miss")
	if err != nil {
		t.Fatal(err)
	}
	c := cmd.(ReportCommand)
	if !reflect.DeepEqual(c.Filter.Models, []string{"alias", "team/model"}) || !reflect.DeepEqual(c.Filter.Backends, []string{"openai"}) || !reflect.DeepEqual(c.Filter.CacheModes, []domain.CacheMode{domain.CacheMiss}) {
		t.Fatalf("unexpected filters: %+v", c.Filter)
	}
}

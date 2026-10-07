package cli

import (
	"flag"
	"strings"

	"dyno/internal/domain"
	"dyno/internal/endpoint"
)

// APIKey returns the explicitly resolved credential. It is never serialized with
// command options or copied to benchmark/session provenance.
func (c Common) APIKey() string { return c.apiKey }

// BackendName preserves the legacy default for programmatically built commands.
func (c Common) BackendName() string {
	if c.Backend == "" {
		return domain.BackendOllama
	}
	return c.Backend
}

func (c *Common) addProvider(fs *flag.FlagSet) {
	c.addHost(fs)
	fs.StringVar(&c.Backend, "backend", "", "inference protocol: ollama or openai (default: ollama)")
	fs.StringVar(&c.BaseURL, "base-url", "", "full OpenAI-compatible API base `URL`, including its path")
	fs.StringVar(&c.APIKeyEnv, "api-key-env", "", "environment variable `name` for bearer authentication (default: OPENAI_API_KEY)")
	fs.StringVar(&c.ServerKind, "server", "", "declared compatible server `engine`: unknown or vllm (not its version)")
	fs.StringVar(&c.ServerVersion, "server-version", "", "operator-supplied compatible server `version` (max 64 printable characters)")
}

func (c *Common) addEndpointWorkload(fs *flag.FlagSet) {
	fs.StringVar(&c.Usage, "usage", "", "streaming usage policy: required or off (default: required)")
	fs.StringVar(&c.TokenLimitField, "token-limit-field", "", "compatible token-limit field: max_tokens or max_completion_tokens (default: max_tokens)")
	fs.StringVar(&c.ModelRevision, "model-revision", "", "operator-supplied immutable model `revision` (single model; unpadded printable max 64, or sha256:<64 hex>)")
	fs.BoolVar(&c.SkipModelDiscovery, "skip-model-discovery", false, "use supplied IDs without calling the models endpoint")
	fs.BoolVar(&c.OmitSeed, "omit-seed", false, "omit seed for compatible endpoints that reject it; record that policy")
	fs.BoolVar(&c.OmitTemperature, "omit-temperature", false, "omit temperature for compatible endpoints that reject it; record that policy")
}

// resolveProvider runs after flag parsing, so ordering never changes defaults.
func (c *Common) resolveProvider(fs *flag.FlagSet, env Env) error {
	switch c.BackendName() {
	case "ollama":
		for _, name := range []string{"base-url", "api-key-env", "server", "server-version", "usage", "token-limit-field", "model-revision", "skip-model-discovery", "omit-seed", "omit-temperature"} {
			if isSet(fs, name) {
				return usageErrorf("--%s requires --backend openai", name)
			}
		}
		return nil
	case "openai":
	default:
		return usageErrorf("unknown --backend %q: use ollama or openai", c.Backend)
	}
	if isSet(fs, "host") && isSet(fs, "base-url") {
		host, hostErr := endpoint.Parse(c.Host)
		base, baseErr := endpoint.Parse(c.BaseURL)
		if hostErr != nil || baseErr != nil || host.String() != base.String() {
			return usageErrorf("--host and --base-url must not name different endpoints")
		}
	}
	origin := "--base-url"
	if c.BaseURL == "" {
		c.BaseURL = c.Host
		if c.Host != "" {
			origin = "--host"
		}
	}
	if c.BaseURL == "" {
		c.BaseURL = env.getenv("OPENAI_BASE_URL")
		if c.BaseURL != "" {
			origin = "OPENAI_BASE_URL"
		}
	}
	if c.BaseURL == "" {
		c.BaseURL = endpoint.DefaultBaseURL
	}
	u, err := endpoint.Parse(c.BaseURL)
	if err != nil {
		return usageErrorf("%s: %w", origin, err)
	}
	c.BaseURL = u.String()
	c.Host = c.BaseURL
	if c.APIKeyEnv == "" {
		if isSet(fs, "api-key-env") {
			return usageErrorf("--api-key-env requires a variable name")
		}
		c.APIKeyEnv = "OPENAI_API_KEY"
	}
	if !envName(c.APIKeyEnv) {
		return usageErrorf("invalid --api-key-env variable name")
	}
	c.apiKey = env.getenv(c.APIKeyEnv)
	if !domain.IsAPIKey(c.apiKey) {
		return usageErrorf("invalid API key: require valid UTF-8 without controls")
	}
	if isSet(fs, "api-key-env") && c.apiKey == "" {
		return usageErrorf("--api-key-env %s is empty or unset; provision it in the process environment", c.APIKeyEnv)
	}
	if len(c.apiKey) >= 8 && (strings.Contains(u.Path, c.apiKey) || strings.Contains(u.EscapedPath(), c.apiKey)) {
		return usageErrorf("%s path must not contain the authentication key", origin)
	}
	if !domain.IsOperatorText(c.ServerVersion) || strings.TrimSpace(c.ServerVersion) != c.ServerVersion {
		return usageErrorf("--server-version must be unpadded printable text of at most 64 characters")
	}
	if !domain.IsModelRevision(c.ModelRevision) {
		return usageErrorf("--model-revision must be unpadded printable text of at most 64 characters, or sha256:<64 hex>")
	}
	if c.ServerKind == "" {
		c.ServerKind = "unknown"
	}
	if c.ServerKind != "unknown" && c.ServerKind != "vllm" {
		return usageErrorf("unknown --server %q", c.ServerKind)
	}
	if c.Usage == "" {
		c.Usage = "required"
	}
	if c.Usage != "required" && c.Usage != "off" {
		return usageErrorf("--usage must be required or off")
	}
	if c.TokenLimitField == "" {
		c.TokenLimitField = "max_tokens"
	}
	if c.TokenLimitField != "max_tokens" && c.TokenLimitField != "max_completion_tokens" {
		return usageErrorf("unsupported --token-limit-field %q", c.TokenLimitField)
	}
	return nil
}

func envName(name string) bool {
	if name == "" {
		return false
	}
	for i, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || (i > 0 && ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}

func backendName(s string) (string, error) {
	if s != "ollama" && s != "openai" {
		return "", usageErrorf("unknown backend %q", s)
	}
	return s, nil
}

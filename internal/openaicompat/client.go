package openaicompat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"dyno/internal/domain"
	"dyno/internal/endpoint"
	"dyno/internal/llm"
)

const (
	defaultBaseURL          = endpoint.DefaultBaseURL
	defaultDiscoveryTimeout = 30 * time.Second
)

// Config contains resolved endpoint settings. New never reads OPENAI_* variables.
// An empty APIKey means no authentication, even when the environment has a key.
// HTTPClient is copied; redirects are disabled to avoid credential leaks and
// implicit extra inference requests. Its transport and timeout are preserved.
type Config struct {
	BaseURL            string
	APIKey             string
	Usage              string // required (default) or off
	TokenLimitField    string // max_tokens (default) or max_completion_tokens
	ServerKind         string // unknown (default) or vllm; declaration, not detection
	ServerVersion      string // optional operator-supplied fact
	ModelRevision      string // optional immutable operator-supplied revision
	SkipModelDiscovery bool
	HTTPClient         *http.Client
	DiscoveryTimeout   time.Duration // zero defaults to 30s; independent of chat
	OmitSeed           bool
	OmitTemperature    bool
}

// Client implements text chat through the official SDK. It deliberately has no
// managed lifecycle or cache-reset methods: an external server is not ours.
type Client struct {
	chat   openai.ChatCompletionService
	models openai.ModelService
	config Config
	info   llm.ServerInfo
	now    func() time.Time
}

var (
	_ llm.Client           = (*Client)(nil)
	_ llm.InfoProvider     = (*Client)(nil)
	_ llm.NameNormalizer   = (*Client)(nil)
	_ llm.OptionsValidator = (*Client)(nil)
)

// New constructs a client with zero SDK retries and no ambient SDK defaults.
func New(cfg Config) (*Client, error) {
	base, err := endpointURL(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("openai client: %w", err)
	}
	cfg.BaseURL = base.String()
	// Provenance uses the canonical endpoint; only SDK relative routing needs a slash.
	base.Path += "/"
	if base.RawPath != "" {
		base.RawPath += "/"
	}
	if cfg.Usage == "" {
		cfg.Usage = "required"
	}
	if cfg.Usage != "required" && cfg.Usage != "off" {
		return nil, errors.New("openai client: usage must be required or off")
	}
	if cfg.TokenLimitField == "" {
		cfg.TokenLimitField = "max_tokens"
	}
	if cfg.TokenLimitField != "max_tokens" && cfg.TokenLimitField != "max_completion_tokens" {
		return nil, errors.New("openai client: token limit field must be max_tokens or max_completion_tokens")
	}
	if cfg.ServerKind == "" {
		cfg.ServerKind = "unknown"
	}
	if cfg.ServerKind != "unknown" && cfg.ServerKind != "vllm" {
		return nil, errors.New("openai client: server kind must be unknown or vllm")
	}
	if len(cfg.APIKey) >= 8 && (strings.Contains(base.Path, cfg.APIKey) || strings.Contains(base.EscapedPath(), cfg.APIKey)) {
		return nil, errors.New("openai client: API base path must not contain the authentication key")
	}
	if !domain.IsAPIKey(cfg.APIKey) {
		return nil, errors.New("openai client: invalid API key")
	}
	if !domain.IsModelRevision(cfg.ModelRevision) || !domain.IsOperatorText(cfg.ServerVersion) || strings.TrimSpace(cfg.ServerVersion) != cfg.ServerVersion {
		return nil, errors.New("openai client: revision must be unpadded printable text (max 64, or sha256:<64 hex>); server version must be printable text of at most 64 characters")
	}
	if cfg.DiscoveryTimeout < 0 {
		return nil, errors.New("openai client: discovery timeout must not be negative")
	}
	if cfg.DiscoveryTimeout == 0 {
		cfg.DiscoveryTimeout = defaultDiscoveryTimeout
	}
	hc := http.Client{}
	if cfg.HTTPClient != nil {
		hc = *cfg.HTTPClient
	}
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	hc.Transport = guardedTransport{next: transport, sanitize: newSanitizer(cfg.APIKey, base)}
	opts := []option.RequestOption{
		option.WithBaseURL(base.String()), option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(&hc), option.WithMaxRetries(0),
	}
	// Service constructors, unlike NewClient, never read environment defaults.
	// This also avoids org/project/custom headers and cloud credential discovery.
	c := &Client{
		chat:   openai.NewChatCompletionService(opts...),
		models: openai.NewModelService(opts...),
		config: cfg, now: time.Now,
	}
	c.config.APIKey, c.config.HTTPClient = "", nil
	policy, _ := json.Marshal(map[string]any{
		"usage": cfg.Usage, "token_limit_field": cfg.TokenLimitField,
		"omit_seed": cfg.OmitSeed, "omit_temperature": cfg.OmitTemperature,
		"skip_model_discovery":    cfg.SkipModelDiscovery,
		domain.ReasoningPolicyKey: "unknown", domain.TTFTBasisKey: domain.TTFTBasisVisibleContent,
		"completion_tokens_policy": "server-usage-including-reasoning",
	})
	c.info = llm.ServerInfo{
		Backend: domain.BackendOpenAI, ServerKind: cfg.ServerKind,
		Version: cfg.ServerVersion, Endpoint: cfg.BaseURL,
		MeasurementProtocol: domain.ProtocolOpenAIChat, ContextPolicy: "server-managed",
		ConfigJSON: string(policy),
	}
	if cfg.ServerVersion != "" {
		c.info.VersionSource = "operator-supplied"
	}
	return c, nil
}

func endpointURL(raw string) (*url.URL, error) {
	if raw == "" {
		raw = defaultBaseURL
	}
	return endpoint.Parse(raw)
}

// Version returns only configured facts; it never probes /version.
func (c *Client) Version(ctx context.Context) (string, error) {
	return c.info.Version, ctx.Err()
}

// Info returns pure provenance, with unknown context capacity and reasoning policy.
func (c *Client) Info(ctx context.Context) (llm.ServerInfo, error) {
	return c.info, ctx.Err()
}

// NormalizeModelName preserves opaque served IDs, including slashes and colons.
func (*Client) NormalizeModelName(name string) string { return name }

// ValidateOptions rejects promises that generic chat inference cannot enforce.
// Explicit keep-alive is rejected by the CLI; Chat ignores the native implicit
// keep-alive setting rather than sending it to the endpoint.
func (c *Client) ValidateOptions(options llm.GenerationOptions, modes []domain.CacheMode) error {
	if options.ContextSize != 0 {
		return errors.New("openai: context size is server-managed; num-ctx is unsupported")
	}
	if options.MaxTokens <= 0 {
		return errors.New("openai: maximum tokens must be positive")
	}
	if !c.config.OmitTemperature && (math.IsNaN(options.Temperature) || math.IsInf(options.Temperature, 0) || options.Temperature < 0 || options.Temperature > 2) {
		return errors.New("openai: temperature must be from 0 to 2")
	}
	for _, mode := range modes {
		if mode != domain.CacheMiss {
			return errors.New("openai: only cache miss is supported; cache reuse/reset requires live validation")
		}
	}
	return nil
}

// Models lists served IDs. A missing listing route may fall back only to supplied
// IDs; authentication and transport failures are never disguised as discovery.
func (c *Client) Models(ctx context.Context, names ...string) ([]domain.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.config.SkipModelDiscovery {
		return c.suppliedModels(names)
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.DiscoveryTimeout)
	defer cancel()
	ctx, state := withResponseState(ctx)
	page, err := c.models.List(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, mapError(err, false, state)
		}
		if (state.err == nil || errors.Is(state.err, errResponseSize)) && (state.status == http.StatusNotFound || state.status == http.StatusMethodNotAllowed) {
			return c.suppliedModels(names)
		}
		return nil, fmt.Errorf("openai models: %w", mapError(err, false, state))
	}
	if page == nil || !page.JSON.Data.Valid() {
		return nil, protocolError("invalid model listing")
	}
	models := make([]domain.Model, 0, len(page.Data))
	for _, m := range page.Data {
		if !stringField(m.JSON.ID, m.ID, true) {
			return nil, protocolError("invalid model ID in listing")
		}
		if len(names) == 0 || slices.Contains(names, m.ID) {
			models = append(models, c.model(m.ID))
		}
	}
	return models, nil
}

func (c *Client) suppliedModels(names []string) ([]domain.Model, error) {
	if len(names) == 0 {
		return nil, ErrDiscoveryUnsupported
	}
	models := make([]domain.Model, 0, len(names))
	for _, name := range names {
		if name == "" {
			return nil, errors.New("openai models: model ID must not be empty")
		}
		models = append(models, c.model(name))
	}
	return models, nil
}

func (c *Client) model(name string) domain.Model {
	m := domain.Model{Name: name, MetadataAvailable: &domain.ModelMetadataAvailability{}}
	if c.config.ModelRevision != "" {
		// Length-delimited JSON avoids ambiguous namespace concatenation. Endpoint
		// scope prevents two different deployments from sharing an alias identity.
		namespace, _ := json.Marshal([]string{domain.BackendOpenAI, c.info.Endpoint, name, c.config.ModelRevision})
		sum := sha256.Sum256(namespace)
		m.Identity = domain.BackendOpenAI + ":revision:" + hex.EncodeToString(sum[:])
		m.IdentitySource, m.Revision = "operator-revision", c.config.ModelRevision
	}
	// Without a revision, leave identity empty: the runner assigns session scope.
	return m
}

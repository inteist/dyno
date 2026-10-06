package ollama

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ollama/ollama/api"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// Defaults of a host without a scheme or a port, as for OLLAMA_HOST.
const (
	defaultScheme   = "http"
	defaultHostName = "127.0.0.1"
	defaultPort     = "11434"
)

// Client implements llm.Client with the official Ollama Go client.
type Client struct {
	api *api.Client
	now func() time.Time
}

var _ llm.Client = (*Client)(nil)

// New returns a client for host. An empty host uses OLLAMA_HOST or the
// default address. A host has the form of OLLAMA_HOST, such as "host:port"
// or "http://host:port". The scheme defaults to http and the port to 11434.
func New(host string) (*Client, error) {
	c, err := newAPIClient(host)
	if err != nil {
		return nil, fmt.Errorf("ollama client: %w", err)
	}
	return newClient(c, time.Now), nil
}

func newClient(c *api.Client, now func() time.Time) *Client {
	return &Client{api: c, now: now}
}

func newAPIClient(host string) (*api.Client, error) {
	if host == "" {
		return api.ClientFromEnvironment()
	}
	base, err := hostURL(host)
	if err != nil {
		return nil, err
	}
	return api.NewClient(base, http.DefaultClient), nil
}

// hostURL parses host in the forms and with the default ports of
// OLLAMA_HOST.
func hostURL(host string) (*url.URL, error) {
	s := strings.TrimSpace(host)
	scheme, rest, explicit := strings.Cut(s, "://")
	if !explicit {
		scheme, rest = defaultScheme, s
	}
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("host %q: the scheme must be http or https", host)
	}
	hostPort, path, _ := strings.Cut(rest, "/")
	name, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		name, port = strings.Trim(hostPort, "[]"), ""
	}
	if err := checkPort(port); err != nil {
		return nil, fmt.Errorf("host %q: %w", host, err)
	}
	return &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(cmp.Or(name, defaultHostName), cmp.Or(port, defaultPortOf(scheme, explicit))),
		Path:   path,
	}, nil
}

// defaultPortOf returns the port of a host without one, as OLLAMA_HOST
// does: 11434 without a scheme, else the standard port of the scheme.
func defaultPortOf(scheme string, explicit bool) string {
	switch {
	case !explicit:
		return defaultPort
	case scheme == "https":
		return "443"
	}
	return "80"
}

// checkPort accepts an empty port or a port number.
func checkPort(port string) error {
	if port == "" {
		return nil
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return errors.New("the port must be a number from 0 to 65535")
	}
	return nil
}

// Version returns the server version.
func (c *Client) Version(ctx context.Context) (string, error) {
	v, err := c.api.Version(ctx)
	if err != nil {
		return "", fmt.Errorf("ollama version: %w", mapError(err))
	}
	return v, nil
}

// Models returns the local models, or only the models with names. The
// details come from one Show request for each model, because List
// leaves them empty for safetensors models.
func (c *Client) Models(ctx context.Context, names ...string) ([]domain.Model, error) {
	list, err := c.api.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama list: %w", mapError(err))
	}
	models := make([]domain.Model, 0, len(list.Models))
	for _, m := range selected(list.Models, names) {
		show, err := c.show(ctx, m.Name)
		if err != nil {
			return nil, err
		}
		models = append(models, toModel(m, show))
	}
	return models, nil
}

// selected returns the entries of list with names, or all entries without
// names.
func selected(list []api.ListModelResponse, names []string) []api.ListModelResponse {
	if len(names) == 0 {
		return list
	}
	return slices.DeleteFunc(slices.Clone(list), func(m api.ListModelResponse) bool {
		return !slices.Contains(names, m.Name)
	})
}

func (c *Client) show(ctx context.Context, model string) (*api.ShowResponse, error) {
	show, err := c.api.Show(ctx, &api.ShowRequest{Model: model})
	if err != nil {
		return nil, fmt.Errorf("ollama show %s: %w", model, mapError(err))
	}
	return show, nil
}

// Running returns the names of the loaded models, spelled as in Models.
func (c *Client) Running(ctx context.Context) ([]string, error) {
	ps, err := c.api.ListRunning(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama ps: %w", mapError(err))
	}
	return runningNames(ps), nil
}

// Load loads model and keeps it loaded for keepAlive. The server reports no
// load time for a load request, so Load returns the time of the request.
func (c *Client) Load(ctx context.Context, model string, keepAlive time.Duration, contextSize int) (time.Duration, error) {
	start := c.now()
	r, err := c.generate(ctx, toGenerateRequest(model, keepAlive, contextSize))
	if err != nil {
		return 0, fmt.Errorf("ollama load %s: %w", model, err)
	}
	return loadDuration(r, c.now().Sub(start)), nil
}

// Unload asks the server to unload model.
func (c *Client) Unload(ctx context.Context, model string) error {
	if _, err := c.generate(ctx, toGenerateRequest(model, 0, 0)); err != nil {
		return fmt.Errorf("ollama unload %s: %w", model, err)
	}
	return nil
}

// generate sends req, an empty prompt that loads or unloads a model. It
// returns the final response.
func (c *Client) generate(ctx context.Context, req *api.GenerateRequest) (api.GenerateResponse, error) {
	var final *api.GenerateResponse
	err := c.api.Generate(ctx, req, func(r api.GenerateResponse) error {
		if r.Done {
			final = &r
		}
		return nil
	})
	if err != nil {
		return api.GenerateResponse{}, mapError(err)
	}
	if final == nil {
		return api.GenerateResponse{}, errIncomplete()
	}
	return *final, nil
}

// Chat streams one request. TTFT is the time from the request start to the
// first chunk with content or thinking.
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	res, err := c.chat(ctx, req, progress)
	if err != nil {
		return llm.ChatResult{}, fmt.Errorf("ollama chat %s: %w", req.Model.Name, err)
	}
	return res, nil
}

func (c *Client) chat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	s := newChatStream(c.now(), progress)
	err := c.api.Chat(ctx, toChatRequest(req), func(r api.ChatResponse) error {
		now := c.now() // first statement: no code runs before the TTFT stamp
		s.add(now, r)
		return nil
	})
	if err != nil {
		return llm.ChatResult{}, mapError(err)
	}
	return s.result(c.now())
}

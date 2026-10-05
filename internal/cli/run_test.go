package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"token-rate/internal/bench"
	"token-rate/internal/domain"
	"token-rate/internal/llm"
	"token-rate/internal/prompt"
)

// builtin returns the built-in prompt set name.
func builtin(t *testing.T, name string) prompt.Set {
	t.Helper()
	set, ok := prompt.Builtin(name)
	if !ok {
		t.Fatalf("no built-in prompt %q", name)
	}
	return set
}

// defaultRun is the command of "run --models a".
func defaultRun(t *testing.T) RunCommand {
	t.Helper()
	return RunCommand{
		Common: Common{DB: homeDB},
		Options: bench.Options{
			Models:         []string{"a:latest"},
			Runs:           5,
			Interval:       30 * time.Second,
			Order:          domain.OrderModel,
			CacheModes:     []domain.CacheMode{domain.CacheMiss},
			Prompt:         builtin(t, prompt.Short),
			Generation:     llm.GenerationOptions{MaxTokens: 512, ContextSize: 8192, Temperature: 0, Seed: 42},
			KeepAlive:      10 * time.Minute,
			RequestTimeout: 10 * time.Minute,
			ProgressEvery:  bench.DefaultProgressEvery,
		},
	}
}

// writePromptFile writes a prompt file that serves every mode.
func writePromptFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review.json")
	content := `{"system": "s", "question": "q", "follow_up": "f"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseRun(t *testing.T) {
	t.Parallel()
	file := writePromptFile(t)
	long := builtin(t, prompt.Long)
	tests := []struct {
		name   string
		args   []string
		change func(*RunCommand)
	}{
		{"defaults", []string{"--models", "a"}, func(*RunCommand) {}},
		{"every flag", []string{
			"--models", "a,b", "--runs", "3", "--interval", "1m", "--cache", "miss,prefix",
			"--prompt", "long", "--num-predict", "256", "--num-ctx", "4096", "--temperature", "0.5",
			"--seed", "7", "--order", "round-robin", "--keep-alive", "30m", "--request-timeout", "2m",
			"--tag", "baseline", "--plain", "--db", "/data/x.db", "--host", "http://mac:11434", "--log", "/tmp/run.log",
		}, func(c *RunCommand) {
			c.Common = Common{DB: "/data/x.db", Host: "http://mac:11434", LogFile: "/tmp/run.log"}
			c.Plain = true
			o := &c.Options
			o.Models, o.Runs, o.Interval = []string{"a:latest", "b:latest"}, 3, time.Minute
			o.CacheModes = []domain.CacheMode{domain.CacheMiss, domain.CachePrefix}
			o.Prompt = long
			o.Generation = llm.GenerationOptions{MaxTokens: 256, ContextSize: 4096, Temperature: 0.5, Seed: 7}
			o.Order, o.KeepAlive, o.RequestTimeout, o.Tag = domain.OrderRoundRobin, 30*time.Minute, 2*time.Minute, "baseline"
		}},
		{"list with spaces and empty items", []string{"--models", " a , b,, "}, func(c *RunCommand) {
			c.Options.Models = []string{"a:latest", "b:latest"}
		}},
		{"the last flag wins", []string{"--models", "x,y", "-models=a"}, func(*RunCommand) {}},
		{"miss gives short", []string{"--models", "a", "--cache", "miss"}, func(*RunCommand) {}},
		{"continue gives long", []string{"--models", "a", "--cache", "continue"}, func(c *RunCommand) {
			c.Options.CacheModes, c.Options.Prompt = []domain.CacheMode{domain.CacheContinue}, long
		}},
		{"prefix gives long", []string{"--models", "a", "--cache", "miss,prefix"}, func(c *RunCommand) {
			c.Options.CacheModes, c.Options.Prompt = []domain.CacheMode{domain.CacheMiss, domain.CachePrefix}, long
		}},
		{"explicit prompt", []string{"--models", "a", "--prompt", "long"}, func(c *RunCommand) {
			c.Options.Prompt = long
		}},
		{"prompt file", []string{"--models", "a", "--prompt", file, "--cache", "continue"}, func(c *RunCommand) {
			c.Options.CacheModes = []domain.CacheMode{domain.CacheContinue}
			c.Options.Prompt = prompt.Set{Name: "review", System: "s", Question: "q", FollowUp: "f"}
		}},
		{"keep-alive follows the interval", []string{"--models", "a", "--interval", "20m"}, func(c *RunCommand) {
			c.Options.Interval, c.Options.KeepAlive = 20*time.Minute, 25*time.Minute
		}},
		{"explicit keep-alive", []string{"--models", "a", "--keep-alive", "15m"}, func(c *RunCommand) {
			c.Options.KeepAlive = 15 * time.Minute
		}},
		{"explicit keep-alive with a long interval", []string{"--models", "a", "--interval", "20m", "--keep-alive", "21m"}, func(c *RunCommand) {
			c.Options.Interval, c.Options.KeepAlive = 20*time.Minute, 21*time.Minute
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, stderr, err := parse(testEnv(t, nil), append([]string{"run"}, tt.args...)...)
			if err != nil {
				t.Fatalf("Parse() error = %v, stderr:\n%s", err, stderr)
			}
			want := defaultRun(t)
			tt.change(&want)
			if !reflect.DeepEqual(cmd, want) {
				t.Errorf("Parse() =\n%+v\nwant\n%+v", cmd, want)
			}
		})
	}
}

func TestParseRunErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want error
		text string
	}{
		{"unknown flag", []string{"--models", "a", "--bogus"}, ErrUsage, "not defined: -bogus"},
		{"report flag", []string{"--models", "a", "--group", "model"}, ErrUsage, "not defined: -group"},
		{"unknown cache mode", []string{"--models", "a", "--cache", "miss,hit"}, ErrUsage, `unknown cache mode "hit"`},
		{"unknown order", []string{"--models", "a", "--order", "random"}, ErrUsage, `unknown order "random"`},
		{"bad number", []string{"--models", "a", "--runs", "many"}, ErrUsage, `invalid value "many" for flag -runs`},
		{"duration without unit", []string{"--models", "a", "--interval", "5"}, ErrUsage, `invalid value "5" for flag -interval`},
		{"unknown prompt", []string{"--models", "a", "--prompt", "medium"}, ErrUsage, `run: --prompt: unknown prompt "medium"`},
		{"positional argument", []string{"--models", "a", "now"}, ErrUsage, `unexpected argument "now"`},
		{"no model", nil, bench.ErrInvalidOptions, "at least one model"},
		{"empty model list", []string{"--models", " , "}, bench.ErrInvalidOptions, "at least one model"},
		{"zero runs", []string{"--models", "a", "--runs", "0"}, bench.ErrInvalidOptions, "runs must be 1 or more"},
		{"keep-alive not longer than the interval", []string{"--models", "a", "--keep-alive", "30s"}, bench.ErrInvalidOptions, "keep-alive 30s must be longer"},
		{"prompt without system part", []string{"--models", "a", "--prompt", "short", "--cache", "prefix"}, bench.ErrInvalidOptions, `prompt "short" cannot serve`},
		{"negative temperature", []string{"--models", "a", "--temperature", "-1"}, bench.ErrInvalidOptions, "temperature must not be negative"},
		{"no request timeout", []string{"--models", "a", "--request-timeout", "0s"}, bench.ErrInvalidOptions, "request timeout must be longer than 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, _, err := parse(testEnv(t, nil), append([]string{"run"}, tt.args...)...)
			if !errors.Is(err, tt.want) || cmd != nil {
				t.Fatalf("Parse() = %v, %v, want %v", cmd, err, tt.want)
			}
			if !strings.Contains(err.Error(), tt.text) {
				t.Errorf("error = %q, want %q in it", err, tt.text)
			}
		})
	}
}

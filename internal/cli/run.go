package cli

import (
	"cmp"
	"flag"
	"strings"
	"time"

	"dyno/internal/bench"
	"dyno/internal/domain"
	"dyno/internal/prompt"
)

// RunCommand executes a session.
type RunCommand struct {
	Common
	Options bench.Options
	Plain   bool // plain text, also in a terminal
}

func (RunCommand) command() {}

// Defaults of the run command.
const (
	defaultRuns           = 5
	defaultInterval       = 30 * time.Second
	defaultNumPredict     = 512
	defaultNumCtx         = 8192
	defaultSeed           = 42
	defaultRequestTimeout = 10 * time.Minute
)

// runFlags holds the run command while the flag set parses the arguments.
type runFlags struct {
	cmd    RunCommand
	prompt string // a built-in name or a file; empty means automatic
}

// defineRun registers the flags of the run command.
func defineRun(fs *flag.FlagSet, env Env) builder {
	r := &runFlags{cmd: RunCommand{Options: bench.Options{
		Order:         domain.OrderModel,
		CacheModes:    []domain.CacheMode{domain.CacheMiss},
		ProgressEvery: bench.DefaultProgressEvery,
	}}}
	r.addSchedule(fs)
	r.addWorkload(fs)
	r.cmd.addDB(fs, env)
	r.cmd.addProvider(fs)
	r.cmd.addEndpointWorkload(fs)
	r.cmd.addLog(fs)
	fs.BoolVar(&r.cmd.Plain, "plain", false, "plain text, also in a terminal")
	return func() (Command, error) { return r.build(fs, env) }
}

// addSchedule registers the flags that choose the steps and their timing.
func (r *runFlags) addSchedule(fs *flag.FlagSet) {
	o := &r.cmd.Options
	fs.Var(listValue(&o.Models, asIs[string]), "models", "comma-separated model `names` (required)")
	fs.IntVar(&o.Runs, "runs", defaultRuns, "measured runs of each model and cache mode, 1 or more")
	fs.DurationVar(&o.Interval, "interval", defaultInterval, "pause between two steps, 0 or more")
	fs.Var(listValue(&o.CacheModes, domain.ParseCacheMode), "cache",
		"cache `modes`: "+domain.JoinEnum(domain.CacheModes()))
	fs.Var(orderValue(&o.Order), "order", "step `order`: "+domain.JoinEnum(domain.Orders()))
	fs.DurationVar(&o.KeepAlive, "keep-alive", 0,
		"how long the server keeps a model loaded, longer than the interval (default: max(10m, interval + 5m))")
	fs.DurationVar(&o.RequestTimeout, "request-timeout", defaultRequestTimeout, "limit of one request")
	fs.StringVar(&o.Tag, "tag", "", "printable `text` that marks the session (no controls; unrestricted length)")
}

// addWorkload registers the flags that choose the prompt and the generation
// settings.
func (r *runFlags) addWorkload(fs *flag.FlagSet) {
	g := &r.cmd.Options.Generation
	fs.StringVar(&r.prompt, "prompt", "", "prompt `set`: "+strings.Join(prompt.BuiltinNames(), ", ")+
		", or a JSON file (default: short for --cache miss, else long)")
	fs.IntVar(&g.MaxTokens, "num-predict", defaultNumPredict, "tokens to generate in each request, 1 or more")
	fs.IntVar(&g.ContextSize, "num-ctx", defaultNumCtx, "context size in tokens, 1 or more")
	fs.Float64Var(&g.Temperature, "temperature", 0, "sampling temperature, 0 or more")
	fs.IntVar(&g.Seed, "seed", defaultSeed, "sampling seed")
}

// build fills the automatic defaults and validates the options.
func (r *runFlags) build(fs *flag.FlagSet, env Env) (Command, error) {
	if err := r.cmd.checkDB("run"); err != nil {
		return nil, err
	}
	if err := r.cmd.resolveProvider(fs, env); err != nil {
		return nil, err
	}
	o := &r.cmd.Options
	o.External = r.cmd.BackendName() == "openai"
	if o.External {
		if isSet(fs, "keep-alive") {
			return nil, usageErrorf("--keep-alive is not supported by external endpoints")
		}
		if r.cmd.OmitSeed && isSet(fs, "seed") {
			return nil, usageErrorf("--omit-seed conflicts with explicit --seed")
		}
		if r.cmd.OmitTemperature && isSet(fs, "temperature") {
			return nil, usageErrorf("--omit-temperature conflicts with explicit --temperature")
		}
		if r.cmd.OmitSeed {
			o.Generation.Seed = 0
		}
		if r.cmd.OmitTemperature {
			o.Generation.Temperature = 0
		}
		if isSet(fs, "num-ctx") {
			return nil, usageErrorf("--num-ctx cannot resize an external server; context is server-managed")
		}
		o.Generation.ContextSize = 0
		o.KeepAlive = 0
		if r.cmd.ModelRevision != "" && len(o.Models) != 1 {
			return nil, usageErrorf("--model-revision requires exactly one model")
		}
		for _, mode := range o.CacheModes {
			if mode != domain.CacheMiss {
				return nil, usageErrorf("--backend openai currently supports only --cache miss; cache reuse/reset requires live validation")
			}
		}
	} else {
		o.Models = domain.NormalizeModelNames(o.Models, domain.NormalizeModelName)
		if !isSet(fs, "keep-alive") {
			o.KeepAlive = bench.DefaultKeepAlive(o.Interval)
		}
	}
	set, err := prompt.Resolve(cmp.Or(r.prompt, prompt.DefaultFor(o.CacheModes)))
	if err != nil {
		return nil, usageErrorf("run: --prompt: %w", err)
	}
	o.Prompt = set
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return r.cmd, nil
}

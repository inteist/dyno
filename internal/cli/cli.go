package cli

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"dyno/internal/domain"
)

// ErrUsage means that the arguments are wrong. Parse has printed the usage
// text; the caller prints the error and exits with code 2.
var ErrUsage = errors.New("wrong arguments")

// Command is one of RunCommand, ReportCommand, ModelsCommand, and
// VersionCommand.
type Command interface{ command() }

// Common holds the settings that several commands share.
type Common struct {
	DB      string // --db, dbVar, $XDG_DATA_HOME/dyno/bench.db, or ~/.local/share/dyno/bench.db
	Host    string // --host; empty means that the ollama adapter uses OLLAMA_HOST or its default address
	LogFile string // --log; empty means no log file
}

// VersionCommand prints the tool version.
type VersionCommand struct{}

func (VersionCommand) command() {}

// Env holds the facts of the process that Parse reads. OSEnv gives the real
// ones, and tests fill it by hand.
type Env struct {
	Getenv   func(string) string // nil means an empty environment
	HomeDir  string              // empty when unknown
	Location *time.Location      // the time zone of dates; nil means UTC
}

// OSEnv returns the Env of this process. An unknown home folder leaves
// HomeDir empty, so that only the commands that need it fail.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Getenv: os.Getenv, HomeDir: home, Location: time.Local}
}

// getenv returns the environment variable key.
func (e Env) getenv(key string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(key)
}

// location returns the time zone of dates.
func (e Env) location() *time.Location { return cmp.Or(e.Location, time.UTC) }

// dbVar is the environment variable that names the database path.
const dbVar = "DYNO_DB"

// defaultDB returns the database path without --db, or "" when no variable
// names a path and the home folder is unknown.
func (e Env) defaultDB() string {
	if db := e.getenv(dbVar); db != "" {
		return db
	}
	dataHome := e.getenv("XDG_DATA_HOME")
	if dataHome == "" && e.HomeDir != "" {
		dataHome = filepath.Join(e.HomeDir, ".local", "share")
	}
	if dataHome == "" {
		return ""
	}
	return filepath.Join(dataHome, domain.ToolName, "bench.db")
}

// Parse reads the arguments without the program name and returns the
// command to execute. For wrong arguments, it prints the usage text on
// stderr and returns an error that wraps ErrUsage. For -h, --help, help, or
// no arguments, it prints the usage text and returns an error that wraps
// flag.ErrHelp. Invalid run options give the error of bench.Options.Validate,
// without the usage text. Parse never prints an error message.
func Parse(args []string, env Env, stderr io.Writer) (Command, error) {
	if len(args) == 0 || isHelp(args[0]) {
		writeOverview(stderr)
		return nil, flag.ErrHelp
	}
	spec, ok := findCommand(args[0])
	if !ok {
		writeOverview(stderr)
		return nil, usageErrorf("unknown command %q", args[0])
	}
	return spec.parse(args[1:], env, stderr)
}

// isHelp reports whether arg asks for the overview.
func isHelp(arg string) bool {
	return slices.Contains([]string{"help", "-h", "-help", "--help"}, arg)
}

// usageError marks an error as wrong arguments. Its message is the message
// of err alone.
type usageError struct{ err error }

func (e usageError) Error() string   { return e.err.Error() }
func (e usageError) Unwrap() []error { return []error{ErrUsage, e.err} }

// usageErrorf returns a formatted error that wraps ErrUsage.
func usageErrorf(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

// builder returns the command after the flag set parsed the arguments.
type builder func() (Command, error)

// commandSpec describes one command.
type commandSpec struct {
	name    string
	args    string // the synopsis after the name
	summary string
	// define registers the flags of the command on fs.
	define func(fs *flag.FlagSet, env Env) builder
}

// commands returns every command in the order of the usage text.
func commands() []commandSpec {
	return []commandSpec{
		{"run", "--models LIST [options]", "execute a session", defineRun},
		{"report", "[options]", "summarize the stored runs", defineReport},
		{"models", "[--host URL]", "list the local models", defineModels},
		{"version", "", "print the tool version", defineVersion},
	}
}

// findCommand returns the command with name.
func findCommand(name string) (commandSpec, bool) {
	all := commands()
	i := slices.IndexFunc(all, func(c commandSpec) bool { return c.name == name })
	if i < 0 {
		return commandSpec{}, false
	}
	return all[i], true
}

// parse reads the arguments of the command. It prints the usage text for
// every error that wraps ErrUsage or flag.ErrHelp.
func (c commandSpec) parse(args []string, env Env, stderr io.Writer) (Command, error) {
	fs := flag.NewFlagSet(c.name, flag.ContinueOnError)
	// The caller prints the error, and this method prints the usage text.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	cmd, err := parseFlags(fs, args, c.define(fs, env))
	if errors.Is(err, ErrUsage) || errors.Is(err, flag.ErrHelp) {
		c.writeUsage(stderr, fs)
	}
	return cmd, err
}

// parseFlags parses args with fs and builds the command.
func parseFlags(fs *flag.FlagSet, args []string, build builder) (Command, error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, usageErrorf("%s: %w", fs.Name(), err)
	}
	if fs.NArg() > 0 {
		return nil, usageErrorf("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return build()
}

// defineVersion registers no flags.
func defineVersion(*flag.FlagSet, Env) builder {
	return func() (Command, error) { return VersionCommand{}, nil }
}

// addDB registers --db.
func (c *Common) addDB(fs *flag.FlagSet, env Env) {
	fs.StringVar(&c.DB, "db", env.defaultDB(), "database `path`")
}

// checkDB fails when the database has no path, because the home folder is
// unknown and nothing else names a path.
func (c Common) checkDB(command string) error {
	if c.DB == "" {
		return usageErrorf("%s: the home folder is unknown: set --db or %s", command, dbVar)
	}
	return nil
}

// addHost registers --host.
func (c *Common) addHost(fs *flag.FlagSet) {
	fs.StringVar(&c.Host, "host", "", "Ollama server `URL` (default: OLLAMA_HOST or http://127.0.0.1:11434)")
}

// addLog registers --log.
func (c *Common) addLog(fs *flag.FlagSet) {
	fs.StringVar(&c.LogFile, "log", "", "`file` that gets a plain-text copy of the session events")
}

// isSet reports whether the arguments set the flag name.
func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// writeOverview prints the usage text of the program.
func writeOverview(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s <command> [options]\n\nCommands:\n", domain.ToolName)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, c := range commands() {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", c.name, c.args, c.summary)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "\nRun '%s <command> -h' for the options of a command.\n", domain.ToolName)
}

// writeUsage prints the usage text of the command with the flags of fs.
func (c commandSpec) writeUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintf(w, "%[1]s %[2]s: %[3]s\n\nUsage: %[1]s %[4]s\n\nOptions:\n",
		domain.ToolName, c.name, c.summary, strings.TrimSpace(c.name+" "+c.args))
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fs.VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(tw, "  %s\t%s%s\n", strings.TrimSpace("--"+f.Name+" "+arg), usage, defaultNote(f.DefValue))
	})
	fmt.Fprint(tw, "  -h, --help\tshow this help\n")
	_ = tw.Flush()
}

// defaultNote returns the default for the usage text. Zero values show no
// default, like in the flag package.
func defaultNote(def string) string {
	switch def {
	case "", "0", "0s", "false":
		return ""
	}
	return " (default " + def + ")"
}

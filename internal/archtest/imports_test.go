package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const module = "dyno"

// rule names the module packages and the third-party path prefixes that a
// package is permitted to import. A prefix matches itself and every path
// below it. The standard library is always permitted.
type rule struct {
	internal   []string
	thirdParty []string
}

var (
	uiInternal    = []string{"domain", "event", "report", "stats", "ui/format"}
	tuiInternal   = append(slices.Clone(uiInternal), "ui/tui/theme", "ui/tui/runview", "ui/tui/reportview")
	tuiThirdParty = []string{"charm.land", "github.com/NimbleMarkets/ntcharts", "github.com/charmbracelet"}
)

// rules is section 7.3 of the plan. A package that is not in rules fails the
// test, so that each new package gets a rule.
func rules() map[string]rule {
	return map[string]rule{
		"internal/domain":            {},
		"internal/stats":             {},
		"internal/archtest":          {},
		"internal/golden":            {}, // only tests import it
		"internal/llm":               {internal: []string{"domain"}},
		"internal/llm/llmtest":       {internal: []string{"domain", "llm"}},
		"internal/prompt":            {internal: []string{"domain"}},
		"internal/event":             {internal: []string{"domain"}},
		"internal/event/eventtest":   {internal: []string{"event"}},
		"internal/scenario":          {internal: []string{"domain", "prompt"}},
		"internal/bench":             {internal: []string{"domain", "llm", "prompt", "scenario", "event"}},
		"internal/bench/benchtest":   {internal: []string{"domain"}},
		"internal/report":            {internal: []string{"domain", "stats"}},
		"internal/ollama":            {internal: []string{"domain", "llm"}, thirdParty: []string{"github.com/ollama/ollama"}},
		"internal/store/sqlite":      {internal: []string{"domain", "report"}, thirdParty: []string{"modernc.org/sqlite"}},
		"internal/sysinfo":           {internal: []string{"domain"}},
		"internal/ui/format":         {internal: []string{"domain", "report", "stats"}},
		"internal/ui/plain":          {internal: uiInternal},
		"internal/ui/tui":            {internal: tuiInternal, thirdParty: tuiThirdParty},
		"internal/ui/tui/theme":      {thirdParty: tuiThirdParty},
		"internal/ui/tui/runview":    {internal: tuiInternal, thirdParty: tuiThirdParty},
		"internal/ui/tui/reportview": {internal: tuiInternal, thirdParty: tuiThirdParty},
		"internal/cli":               {internal: []string{"domain", "llm", "prompt", "bench", "report"}},
		"internal/app":               {internal: []string{"*"}, thirdParty: []string{"golang.org/x/term"}},
		"cmd/dyno":                   {internal: []string{"cli", "app"}},
	}
}

type pkg struct {
	ImportPath string
	Imports    []string
}

func TestImportRules(t *testing.T) {
	t.Parallel()
	all := rules()
	for _, p := range listPackages(t) {
		name := strings.TrimPrefix(p.ImportPath, module+"/")
		r, ok := all[name]
		if !ok {
			t.Errorf("%s has no rule in archtest", name)
			continue
		}
		for _, imp := range p.Imports {
			if err := r.check(imp); err != nil {
				t.Errorf("%s imports %s: %v", name, imp, err)
			}
		}
	}
}

func (r rule) check(imp string) error {
	switch {
	case strings.HasPrefix(imp, module+"/"):
		target := strings.TrimPrefix(strings.TrimPrefix(imp, module+"/"), "internal/")
		if slices.Contains(r.internal, "*") || slices.Contains(r.internal, target) {
			return nil
		}
		return errors.New("forbidden module package")
	case isStandard(imp):
		return nil
	case slices.ContainsFunc(r.thirdParty, func(prefix string) bool { return within(imp, prefix) }):
		return nil
	}
	return errors.New("forbidden third-party package")
}

// within reports whether path is prefix or a package below prefix.
func within(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// isStandard reports whether path is in the standard library. Only the
// standard library has paths without a dot in the first element.
func isStandard(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func listPackages(t *testing.T) []pkg {
	t.Helper()
	out, err := exec.Command("go", "list", "-e", "-json", module+"/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs
		}
		if err != nil {
			t.Fatalf("decode go list: %v", err)
		}
		pkgs = append(pkgs, p)
	}
}

func TestRuleCheck(t *testing.T) {
	t.Parallel()
	r := rule{internal: []string{"domain"}, thirdParty: []string{"modernc.org/sqlite"}}
	for imp, ok := range map[string]bool{
		"fmt":                          true,
		"dyno/internal/domain":         true,
		"dyno/internal/bench":          false,
		"modernc.org/sqlite":           true,
		"modernc.org/sqlite/lib":       true,
		"modernc.org/sqlitex":          false,
		"github.com/ollama/ollama/api": false,
	} {
		if err := r.check(imp); (err == nil) != ok {
			t.Errorf("check(%s) = %v, want ok = %v", imp, err, ok)
		}
	}
}

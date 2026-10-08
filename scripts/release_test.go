package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitEnv isolates Git from the caller's settings, such as tag signing or a
// GIT_DIR from a hook, and adds extra.
func gitEnv(extra ...string) []string {
	var env []string
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GIT_") {
			env = append(env, item)
		}
	}
	env = append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	return append(env, extra...)
}

// runIn runs name in dir and returns its combined output.
func runIn(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// releaseRepo returns a Git repository with one commit: a main module named
// dyno, as in this repository, that prints the version Go stamps into it.
func releaseRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"go.mod":  "module dyno\n\ngo 1.27\n",
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"runtime/debug\"\n)\n\nfunc main() {\n\tinfo, _ := debug.ReadBuildInfo()\n\tfmt.Print(info.Main.Version)\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-qm", "initial"}} {
		if out, err := runIn(repo, gitEnv(), "git", args...); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return repo
}

// release runs release.sh in repo.
func release(t *testing.T, repo string, env []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("release.sh")
	if err != nil {
		t.Fatal(err)
	}
	return runIn(repo, gitEnv(env...), "bash", append([]string{script}, args...)...)
}

// tags returns the tag names and objects of repo.
func tags(t *testing.T, repo string) string {
	t.Helper()
	out, err := runIn(repo, gitEnv(), "git", "for-each-ref", "refs/tags")
	if err != nil {
		t.Fatalf("list tags: %v: %s", err, out)
	}
	return out
}

func TestReleaseTagsTheVersionGoReports(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.1.0", "1.20.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			repo := releaseRepo(t)
			if out, err := release(t, repo, nil, version); err != nil {
				t.Fatalf("release: %v: %s", err, out)
			}
			binary := filepath.Join(t.TempDir(), "dyno")
			if out, err := runIn(repo, gitEnv(), "go", "build", "-o", binary, "."); err != nil {
				t.Fatalf("build: %v: %s", err, out)
			}
			if got, err := runIn(repo, nil, binary); err != nil || got != "v"+version {
				t.Fatalf("built version = %q, %v; want v%s", got, err, version)
			}
		})
	}
}

func TestReleaseRejectsUnsupportedVersions(t *testing.T) {
	t.Parallel()
	repo := releaseRepo(t)
	for _, args := range [][]string{
		{}, {"0.2.0", "0.3.0"}, {""},
		{"2.0.0"}, {"10.0.0"}, // Go ignores v2+ tags for module path dyno.
		{"01.2.3"}, {"0.02.1"}, {"0.2.01"},
		{"v1.2.3"}, {"1.2"}, {"1.2.3-rc.1"}, {"1.2.3x"},
		{"0.2.0\nfoo"}, {"foo\n0.2.0"}, {"0.2.0\n"},
		{"$(touch ran)0.2.1"}, {"`touch ran`0.2.1"}, {"0.2.1;touch ran"},
	} {
		if out, err := release(t, repo, nil, args...); err == nil {
			t.Errorf("release %q succeeded: %s", args, out)
		}
		if _, err := os.Stat(filepath.Join(repo, "ran")); err == nil {
			t.Fatalf("release %q ran shell text from its argument", args)
		}
	}
	if got := tags(t, repo); got != "" {
		t.Fatalf("rejected versions created tags:\n%s", got)
	}
}

func TestReleaseRefusesUnsafeStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, repo string)
		env   []string
	}{
		{"uncommitted change", func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"untracked file", func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "notes.txt"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"untracked file hidden by config", func(t *testing.T, repo string) {
			if out, err := runIn(repo, gitEnv(), "git", "config", "status.showUntrackedFiles", "no"); err != nil {
				t.Fatalf("git config: %v: %s", err, out)
			}
			if err := os.WriteFile(filepath.Join(repo, "notes.txt"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"git status fails", nil, []string{"GIT_INDEX_FILE=/dev/null"}},
		{"tag exists", func(t *testing.T, repo string) {
			if out, err := release(t, repo, nil, "0.1.0"); err != nil {
				t.Fatalf("first release: %v: %s", err, out)
			}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := releaseRepo(t)
			if tt.setup != nil {
				tt.setup(t, repo)
			}
			before := tags(t, repo)
			if out, err := release(t, repo, tt.env, "0.1.0"); err == nil {
				t.Fatalf("release succeeded: %s", out)
			}
			if after := tags(t, repo); after != before {
				t.Fatalf("tags changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

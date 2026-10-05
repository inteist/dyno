package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseModelsAndVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want Command
	}{
		{"models", []string{"models"}, ModelsCommand{}},
		{"models with a host", []string{"models", "--host", "http://mac:11434"}, ModelsCommand{Common{Host: "http://mac:11434"}}},
		{"version", []string{"version"}, VersionCommand{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, _, err := parse(testEnv(t, nil), tt.args...)
			if err != nil || !reflect.DeepEqual(cmd, tt.want) {
				t.Errorf("Parse() = %+v, %v, want %+v", cmd, err, tt.want)
			}
		})
	}
}

func TestParseModelsAndVersionErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		text string
	}{
		{"models with a database", []string{"models", "--db", "/x.db"}, "not defined: -db"},
		{"models with an argument", []string{"models", "qwen"}, `unexpected argument "qwen"`},
		{"version with a flag", []string{"version", "--short"}, "not defined: -short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, _, err := parse(testEnv(t, nil), tt.args...)
			if !errors.Is(err, ErrUsage) || cmd != nil || !strings.Contains(err.Error(), tt.text) {
				t.Errorf("Parse() = %v, %v, want ErrUsage with %q", cmd, err, tt.text)
			}
		})
	}
}

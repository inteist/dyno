package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"token-rate/internal/domain"
)

func TestParseList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{" , ,, ", nil},
		{"a", []string{"a"}},
		{"a,b", []string{"a", "b"}},
		{" a , ,b ,", []string{"a", "b"}},
		{"qwen3.5:9b-mlx,lfm2.5:8b", []string{"qwen3.5:9b-mlx", "lfm2.5:8b"}},
	}
	for _, tt := range tests {
		got, err := parseList(tt.in, asIs[string])
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseList(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestParseListStopsAtTheFirstError(t *testing.T) {
	t.Parallel()
	_, err := parseList("miss,hit,cold", domain.ParseCacheMode)
	if err == nil || !strings.Contains(err.Error(), `"hit"`) {
		t.Errorf("parseList() error = %v, want the error of hit", err)
	}
}

func TestValueKeepsTheOldValueOnError(t *testing.T) {
	t.Parallel()
	modes := []domain.CacheMode{domain.CacheMiss}
	v := listValue(&modes, domain.ParseCacheMode)
	if err := v.Set("prefix,hit"); err == nil {
		t.Fatal("Set() error = nil")
	}
	if v.String() != "miss" || len(modes) != 1 {
		t.Errorf("after a failed Set: String() = %q, modes = %v", v.String(), modes)
	}
	if err := v.Set("continue, prefix"); err != nil || v.String() != "continue, prefix" {
		t.Errorf("Set() = %v, String() = %q", err, v.String())
	}
}

func TestValueStringOfZeroValue(t *testing.T) {
	t.Parallel()
	var nilValue *value[int]
	if nilValue.String() != "" || (&value[int]{}).String() != "" {
		t.Error("String() of a zero value is not empty")
	}
}

func TestOrderValue(t *testing.T) {
	t.Parallel()
	order := domain.OrderModel
	v := orderValue(&order)
	if err := v.Set("round-robin"); err != nil || order != domain.OrderRoundRobin || v.String() != "round-robin" {
		t.Errorf("Set(round-robin) = %v, order = %q", err, order)
	}
	if err := v.Set("random"); err == nil || order != domain.OrderRoundRobin {
		t.Errorf("Set(random) = %v, order = %q", err, order)
	}
}

func TestDateValueString(t *testing.T) {
	t.Parallel()
	var since time.Time
	v := dateValue(&since, berlin(t), sinceExtraDays)
	if v.String() != "" {
		t.Errorf("String() of the zero time = %q", v.String())
	}
	if err := v.Set("2026-10-05"); err != nil || v.String() != "2026-10-05T00:00:00+02:00" {
		t.Errorf("Set() = %v, String() = %q", err, v.String())
	}
	if err := v.Set("noon"); err == nil || v.String() != "2026-10-05T00:00:00+02:00" {
		t.Errorf("Set(noon) = %v, String() = %q", err, v.String())
	}
}

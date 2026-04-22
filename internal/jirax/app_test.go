package jirax

import (
	"testing"
	"time"
)

func TestCombineJQL(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		extra string
		want  string
	}{
		{name: "scope only", scope: `project in (DEMO, PLAT)`, want: `project in (DEMO, PLAT)`},
		{name: "extra only", extra: `status = Done`, want: `status = Done`},
		{name: "both", scope: `project = DEMO`, extra: `status = Done`, want: `(project = DEMO) AND (status = Done)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := combineJQL(tt.scope, tt.extra); got != tt.want {
				t.Fatalf("combineJQL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecideFreshnessAction(t *testing.T) {
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	cfg := SyncConfig{
		CheckIntervalMinutes: 15,
		MaxStalenessMinutes:  240,
		AllowStaleOnError:    true,
	}

	tests := []struct {
		name     string
		lastSync time.Time
		want     freshnessAction
	}{
		{name: "never synced", lastSync: time.Time{}, want: freshnessActionSync},
		{name: "fresh cache", lastSync: now.Add(-10 * time.Minute), want: freshnessActionSkip},
		{name: "check threshold", lastSync: now.Add(-20 * time.Minute), want: freshnessActionCheck},
		{name: "max stale threshold", lastSync: now.Add(-5 * time.Hour), want: freshnessActionSync},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideFreshnessAction(now, tt.lastSync, cfg); got != tt.want {
				t.Fatalf("decideFreshnessAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

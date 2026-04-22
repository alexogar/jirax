package jirax

import "testing"

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

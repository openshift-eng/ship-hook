package explorer

import (
	"testing"
	"time"
)

func TestShouldShow(t *testing.T) {
	tests := []struct {
		outcome Outcome
		level   ShowLevel
		want    bool
	}{
		{OutcomeActed, ShowActed, true},
		{OutcomeActed, ShowDomain, true},
		{OutcomeActed, ShowAll, true},

		{OutcomeNoChange, ShowActed, false},
		{OutcomeNoChange, ShowDomain, true},
		{OutcomeNoChange, ShowAll, true},

		{OutcomeFiltered, ShowActed, false},
		{OutcomeFiltered, ShowDomain, false},
		{OutcomeFiltered, ShowAll, true},

		{OutcomeSkipped, ShowActed, false},
		{OutcomeSkipped, ShowDomain, false},
		{OutcomeSkipped, ShowAll, true},

		{OutcomeErrored, ShowActed, true},
		{OutcomeErrored, ShowDomain, true},
		{OutcomeErrored, ShowAll, true},
	}

	for _, tc := range tests {
		t.Run(string(tc.outcome), func(t *testing.T) {
			if got := ShouldShow(tc.outcome, tc.level); got != tc.want {
				t.Errorf("ShouldShow(%q, %d) = %v, want %v", tc.outcome, tc.level, got, tc.want)
			}
		})
	}
}

func TestFilterMatches(t *testing.T) {
	base := &Event{
		Time: time.Date(2026, 5, 28, 20, 0, 0, 0, time.UTC),
		Org:  "openshift",
		Repo: "sippy",
		PR:   3557,
	}

	tests := []struct {
		name   string
		filter Filter
		want   bool
	}{
		{"empty filter matches all", Filter{}, true},
		{"matching org", Filter{Org: "openshift"}, true},
		{"wrong org", Filter{Org: "kubernetes"}, false},
		{"matching repo", Filter{Repo: "sippy"}, true},
		{"wrong repo", Filter{Repo: "origin"}, false},
		{"matching PR", Filter{PR: 3557}, true},
		{"wrong PR", Filter{PR: 9999}, false},
		{"since before event", Filter{Since: time.Date(2026, 5, 28, 19, 0, 0, 0, time.UTC)}, true},
		{"since after event", Filter{Since: time.Date(2026, 5, 28, 21, 0, 0, 0, time.UTC)}, false},
		{"until after event", Filter{Until: time.Date(2026, 5, 28, 21, 0, 0, 0, time.UTC)}, true},
		{"until before event", Filter{Until: time.Date(2026, 5, 28, 19, 0, 0, 0, time.UTC)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.Matches(base); got != tc.want {
				t.Errorf("Filter.Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseShowLevel(t *testing.T) {
	tests := []struct {
		input string
		want  ShowLevel
		ok    bool
	}{
		{"acted", ShowActed, true},
		{"domain", ShowDomain, true},
		{"all", ShowAll, true},
		{"invalid", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := ParseShowLevel(tc.input)
			if ok != tc.ok {
				t.Fatalf("ParseShowLevel(%q) ok = %v, want %v", tc.input, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("ParseShowLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

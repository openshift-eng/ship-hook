package main

import "testing"

func TestIsKnownHumanRedHatter(t *testing.T) {
	known := func(string) bool { return true }
	unknown := func(string) bool { return false }
	for _, tc := range []struct {
		name       string
		login      string
		membership func(string) bool
		allowed    bool
	}{
		{"known human", "alice", known, true},
		{"mixed-case human", "Alice", known, true},
		{"unknown human", "outsider", unknown, false},
		{"missing membership", "alice", nil, false},
		{"empty login", "", known, false},
		{"designated bot known to directory", "redhat-chai-bot", known, false},
		{"mixed-case designated bot", "RedHat-Chai-Bot", known, false},
		{"app known to directory", "app[bot]", known, false},
		{"mixed-case app suffix", "app[BOT]", known, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isKnownHumanRedHatter(tc.membership)(tc.login); got != tc.allowed {
				t.Errorf("approval permission for %q = %v, want %v", tc.login, got, tc.allowed)
			}
		})
	}
}

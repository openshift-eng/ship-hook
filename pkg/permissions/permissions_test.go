package permissions

import (
	"strings"
	"testing"
)

func TestStubChecker(t *testing.T) {
	testCases := []struct {
		name     string
		fn       func(string) string
		user     string
		action   string
		resource string
		expected bool
	}{
		{
			name:     "coderabbit access granted",
			fn:       func(string) string { return "employee" },
			user:     "rh-employee",
			action:   "access",
			resource: "coderabbit",
			expected: true,
		},
		{
			name:     "coderabbit access denied",
			fn:       func(string) string { return "" },
			user:     "external-user",
			action:   "access",
			resource: "coderabbit",
			expected: false,
		},
		{
			name:     "coderabbit access with nil function",
			fn:       nil,
			user:     "anyone",
			action:   "access",
			resource: "coderabbit",
			expected: false,
		},
		{
			name: "passes user to function",
			fn: func(login string) string {
				if login == "allowed-user" {
					return "employee"
				}
				return ""
			},
			user:     "allowed-user",
			action:   "access",
			resource: "coderabbit",
			expected: true,
		},
		{
			name:     "unknown action denied",
			fn:       func(string) string { return "employee" },
			user:     "anyone",
			action:   "write",
			resource: "coderabbit",
			expected: false,
		},
		{
			name:     "unknown resource denied",
			fn:       func(string) string { return "employee" },
			user:     "anyone",
			action:   "access",
			resource: "something-else",
			expected: false,
		},
		{
			name:     "unknown action and resource denied",
			fn:       func(string) string { return "employee" },
			user:     "anyone",
			action:   "admin",
			resource: "cluster",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checker := NewStub(tc.fn)
			got := checker.IsAllowed(tc.user, tc.action, tc.resource)
			if got != tc.expected {
				t.Errorf("IsAllowed(%q, %q, %q) = %v, want %v", tc.user, tc.action, tc.resource, got, tc.expected)
			}
		})
	}
}

func TestStubApprovalPolicy(t *testing.T) {
	lookup := func(user string) string {
		// Even erroneous directory entries must not grant approval to known bots.
		switch strings.ToLower(user) {
		case "rh-employee", "redhat-chai-bot", "app[bot]":
			return "employee"
		}
		return ""
	}
	for _, tc := range []struct {
		name    string
		user    string
		lookup  func(string) string
		allowed bool
	}{
		{"known employee", "rh-employee", lookup, true},
		{"outsider", "outsider", lookup, false},
		{"missing Cyborg", "rh-employee", nil, false},
		{"empty login", "", lookup, false},
		{"bot account", "redhat-chai-bot", lookup, false},
		{"bot account case insensitive", "RedHat-Chai-Bot", lookup, false},
		{"app", "app[bot]", lookup, false},
		{"app case insensitive", "App[BOT]", lookup, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := NewStub(tc.lookup)
			if got := checker.IsAllowed(tc.user, "approve", "pr"); got != tc.allowed {
				t.Errorf("approval allowed = %v, want %v", got, tc.allowed)
			}
			if checker.IsAllowed(tc.user, "approve", "coderabbit") || checker.IsAllowed(tc.user, "access", "pr") {
				t.Fatal("permissions leaked to a different action/resource pair")
			}
		})
	}
}

func TestStubCheckerSameIdentity(t *testing.T) {
	lookup := func(user string) string {
		switch strings.ToLower(user) {
		case "alice", "alice-alt":
			return "employee-1"
		case "bob":
			return "employee-2"
		}
		return ""
	}
	for _, tc := range []struct {
		name        string
		user, other string
		lookup      func(string) string
		same        bool
	}{
		{"same account", "alice", "alice", lookup, true},
		{"same account different case", "Alice", "alice", lookup, true},
		{"two accounts one employee", "alice", "alice-alt", lookup, true},
		{"different employees", "alice", "bob", lookup, false},
		{"unknown account", "alice", "outsider", lookup, false},
		{"two unknown accounts", "outsider", "other-outsider", lookup, false},
		{"same account without Cyborg", "Alice", "alice", nil, true},
		{"different accounts without Cyborg", "alice", "alice-alt", nil, false},
		{"empty account", "", "", lookup, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := NewStub(tc.lookup)
			if got := checker.SameIdentity(tc.user, tc.other); got != tc.same {
				t.Errorf("SameIdentity(%q, %q) = %v, want %v", tc.user, tc.other, got, tc.same)
			}
		})
	}
}

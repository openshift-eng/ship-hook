package permissions

import "testing"

func TestStubChecker(t *testing.T) {
	testCases := []struct {
		name     string
		fn       func(string) bool
		user     string
		action   string
		resource string
		expected bool
	}{
		{
			name:     "coderabbit access granted",
			fn:       func(string) bool { return true },
			user:     "rh-employee",
			action:   "access",
			resource: "coderabbit",
			expected: true,
		},
		{
			name:     "coderabbit access denied",
			fn:       func(string) bool { return false },
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
			name:     "passes user to function",
			fn:       func(login string) bool { return login == "allowed-user" },
			user:     "allowed-user",
			action:   "access",
			resource: "coderabbit",
			expected: true,
		},
		{
			name:     "unknown action denied",
			fn:       func(string) bool { return true },
			user:     "anyone",
			action:   "write",
			resource: "coderabbit",
			expected: false,
		},
		{
			name:     "unknown resource denied",
			fn:       func(string) bool { return true },
			user:     "anyone",
			action:   "access",
			resource: "something-else",
			expected: false,
		},
		{
			name:     "unknown action and resource denied",
			fn:       func(string) bool { return true },
			user:     "anyone",
			action:   "admin",
			resource: "cluster",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checker := NewStub(map[Operation]func(string) bool{
				{Action: "access", Resource: "coderabbit"}: tc.fn,
			})
			got := checker.IsAllowed(tc.user, tc.action, tc.resource)
			if got != tc.expected {
				t.Errorf("IsAllowed(%q, %q, %q) = %v, want %v", tc.user, tc.action, tc.resource, got, tc.expected)
			}
		})
	}
}

func TestStubConfiguredOperations(t *testing.T) {
	checker := NewStub(map[Operation]func(string) bool{
		{Action: "read", Resource: "repository"}: func(user string) bool { return user == "employee" || user == "reader" },
		{Action: "write", Resource: "issue"}:     func(user string) bool { return user == "employee" },
	})
	for _, testCase := range []struct {
		user, action, resource string
		allowed                bool
	}{
		{"employee", "read", "repository", true},
		{"employee", "write", "issue", true},
		{"outsider", "read", "repository", false},
		{"reader", "read", "repository", true},
		{"reader", "write", "issue", false},
		{"employee", "write", "repository", false},
		{"employee", "read", "issue", false},
		{"employee", "access", "coderabbit", false},
	} {
		t.Run(testCase.user+"/"+testCase.action+"/"+testCase.resource, func(t *testing.T) {
			if got := checker.IsAllowed(testCase.user, testCase.action, testCase.resource); got != testCase.allowed {
				t.Errorf("IsAllowed() = %v, want %v", got, testCase.allowed)
			}
		})
	}
}

func TestStubCheckerUsesInjectedMembership(t *testing.T) {
	for _, login := range []string{"Mixed-Case", "account[bot]", ""} {
		t.Run(login, func(t *testing.T) {
			operation := Operation{Action: "access", Resource: "service"}
			checker := NewStub(map[Operation]func(string) bool{
				operation: func(user string) bool { return user == login },
			})
			if !checker.IsAllowed(login, operation.Action, operation.Resource) {
				t.Fatal("injected predicate did not receive the unchanged login or define membership")
			}
		})
	}
}

func TestStubCheckerSkipsPredicateForUnconfiguredOperations(t *testing.T) {
	for _, operations := range []map[Operation]func(string) bool{
		nil,
		{{Action: "read", Resource: "repository"}: func(string) bool {
			t.Fatal("predicate was queried for an unconfigured operation")
			return true
		}},
	} {
		checker := NewStub(operations)
		if checker.IsAllowed("employee", "write", "repository") {
			t.Fatal("unconfigured operation was allowed")
		}
	}
}

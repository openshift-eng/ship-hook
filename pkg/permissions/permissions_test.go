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
			checker := NewStub(tc.fn)
			got := checker.IsAllowed(tc.user, tc.action, tc.resource)
			if got != tc.expected {
				t.Errorf("IsAllowed(%q, %q, %q) = %v, want %v", tc.user, tc.action, tc.resource, got, tc.expected)
			}
		})
	}
}

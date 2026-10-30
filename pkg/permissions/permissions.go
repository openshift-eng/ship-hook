package permissions

import "strings"

// Checker is a generic authorization interface.
// Future implementations may embed a policy engine like github.com/casbin/casbin.
type Checker interface {
	IsAllowed(user, action, resource string) bool
}

// IdentityChecker also compares identities, allowing callers to require distinct
// users without knowing how accounts map to people.
type IdentityChecker interface {
	Checker
	SameIdentity(user, other string) bool
}

// StubChecker grants CodeRabbit access and PR approval to Cyborg-known Red
// Hatters. All other action/resource combinations are denied. Replace with a
// policy-engine-backed implementation when richer authorization logic is needed.
type StubChecker struct {
	cyborgEmployeeID func(string) string
}

// NewStub accepts a Cyborg lookup returning an employee UID, or an empty string
// for unknown users. Without a lookup, every permission is denied.
func NewStub(cyborgEmployeeID func(string) string) *StubChecker {
	return &StubChecker{cyborgEmployeeID: cyborgEmployeeID}
}

func (s *StubChecker) IsAllowed(user, action, resource string) bool {
	switch {
	case action == "access" && resource == "coderabbit",
		action == "approve" && resource == "pr":
		return s.isCyborgKnownRedHatter(user)
	}
	return false
}

// isCyborgKnownRedHatter is the built-in predicate shared by the stub policies.
func (s *StubChecker) isCyborgKnownRedHatter(user string) bool {
	return user != "" && !strings.EqualFold(user, "redhat-chai-bot") &&
		!strings.HasSuffix(strings.ToLower(user), "[bot]") &&
		s.cyborgEmployeeID != nil && s.cyborgEmployeeID(user) != ""
}

func (s *StubChecker) SameIdentity(user, other string) bool {
	if user == "" || other == "" {
		return false
	}
	if strings.EqualFold(user, other) {
		return true
	}
	if s.cyborgEmployeeID == nil {
		return false
	}
	id := s.cyborgEmployeeID(user)
	return id != "" && id == s.cyborgEmployeeID(other)
}

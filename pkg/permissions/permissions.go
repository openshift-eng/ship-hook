package permissions

// Checker is a generic authorization interface.
// Future implementations may embed a policy engine like github.com/casbin/casbin.
type Checker interface {
	IsAllowed(user, action, resource string) bool
}

// StubChecker is a minimal stub that only handles action="access", resource="coderabbit".
// All other action/resource combinations are denied. Replace with a policy-engine-backed
// implementation when richer authorization logic is needed.
type StubChecker struct {
	coderabbitAccessFn func(string) bool
}

func NewStub(coderabbitAccessFn func(string) bool) *StubChecker {
	return &StubChecker{coderabbitAccessFn: coderabbitAccessFn}
}

func (s *StubChecker) IsAllowed(user, action, resource string) bool {
	if action == "access" && resource == "coderabbit" {
		if s.coderabbitAccessFn == nil {
			return false
		}
		return s.coderabbitAccessFn(user)
	}
	return false
}

package permissions

// Checker is a generic authorization interface.
// Future implementations may embed a policy engine like github.com/casbin/casbin.
type Checker interface {
	IsAllowed(user, action, resource string) bool
}

// Operation identifies an action on a resource.
type Operation struct {
	Action   string
	Resource string
}

// StubChecker grants operations using their configured user predicates.
// Missing operations and nil predicates deny access.
type StubChecker struct {
	allowed map[Operation]func(string) bool
}

// NewStub configures an independent predicate for each operation.
func NewStub(operations map[Operation]func(string) bool) *StubChecker {
	allowed := make(map[Operation]func(string) bool, len(operations))
	for operation, predicate := range operations {
		allowed[operation] = predicate
	}
	return &StubChecker{allowed: allowed}
}

func (checker *StubChecker) IsAllowed(user, action, resource string) bool {
	predicate := checker.allowed[Operation{Action: action, Resource: resource}]
	return predicate != nil && predicate(user)
}

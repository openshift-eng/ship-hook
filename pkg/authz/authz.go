package authz

import (
	"strings"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

// IdentityResolver resolves GitHub identities to organizational data.
// orgdatacore.ServiceInterface satisfies this interface.
type IdentityResolver interface {
	GetEmployeeByGitHubID(githubID string) *orgdatacore.Employee
	GetUserMemberships(uid string) []orgdatacore.MembershipInfo
}

type Resource struct {
	Org  string
	Repo string
}

func Global() Resource               { return Resource{} }
func Org(org string) Resource        { return Resource{Org: org} }
func Repo(org, repo string) Resource { return Resource{Org: org, Repo: repo} }

type principalKind int

const (
	principalUser principalKind = iota
	principalTeamMembers
	principalOrgMembers
	principalPillarMembers
	principalTeamGroupMembers
	principalKnownToCyborg
)

type Principal struct {
	kind  principalKind
	value string
}

func User(githubLogin string) Principal { return Principal{kind: principalUser, value: githubLogin} }
func TeamMembers(name string) Principal { return Principal{kind: principalTeamMembers, value: name} }
func OrgMembers(name string) Principal  { return Principal{kind: principalOrgMembers, value: name} }
func PillarMembers(name string) Principal {
	return Principal{kind: principalPillarMembers, value: name}
}
func TeamGroupMembers(name string) Principal {
	return Principal{kind: principalTeamGroupMembers, value: name}
}
func KnownToCyborg() Principal { return Principal{kind: principalKnownToCyborg} }

type rule struct {
	principal Principal
	operation string
	resource  Resource
}

type Policy struct {
	rules []rule
}

func NewPolicy() *Policy { return &Policy{} }

func (p *Policy) Allow(principal Principal, operation string, resource Resource) *Policy {
	p.rules = append(p.rules, rule{principal: principal, operation: operation, resource: resource})
	return p
}

type Authorizer interface {
	IsAllowed(user, operation string, resource Resource) bool
}

type policyAuthorizer struct {
	policy   *Policy
	resolver IdentityResolver
}

func NewAuthorizer(policy *Policy, resolver IdentityResolver) Authorizer {
	return &policyAuthorizer{policy: policy, resolver: resolver}
}

type allowAllAuthorizer struct{}

func (allowAllAuthorizer) IsAllowed(string, string, Resource) bool { return true }

func NewAllowAllAuthorizer() Authorizer { return allowAllAuthorizer{} }

func (a *policyAuthorizer) IsAllowed(user, operation string, resource Resource) bool {
	if a.policy == nil || a.resolver == nil {
		return false
	}

	user = strings.ToLower(user)
	employee := a.resolver.GetEmployeeByGitHubID(user)
	var memberships []orgdatacore.MembershipInfo
	membershipsLoaded := false

	for _, r := range a.policy.rules {
		if employee != nil && !membershipsLoaded && principalNeedsMemberships(r.principal) {
			memberships = a.resolver.GetUserMemberships(employee.UID)
			membershipsLoaded = true
		}
		if operationMatches(r.operation, operation) &&
			resourceMatches(r.resource, resource) &&
			principalMatches(r.principal, user, employee, memberships) {
			return true
		}
	}

	return false
}

func operationMatches(ruleOp, queryOp string) bool {
	if ruleOp == queryOp {
		return true
	}
	return strings.HasPrefix(queryOp, ruleOp+"::")
}

func resourceMatches(ruleRes, queryRes Resource) bool {
	if ruleRes.Org == "" && ruleRes.Repo == "" {
		return true
	}
	if ruleRes.Org != queryRes.Org {
		return false
	}
	if ruleRes.Repo == "" {
		return true
	}
	return ruleRes.Repo == queryRes.Repo
}

func principalMatches(p Principal, githubLogin string, employee *orgdatacore.Employee, memberships []orgdatacore.MembershipInfo) bool {
	switch p.kind {
	case principalUser:
		return strings.EqualFold(p.value, githubLogin)
	case principalKnownToCyborg:
		return employee != nil
	case principalTeamMembers:
		return hasMembership(memberships, "team", p.value)
	case principalOrgMembers:
		return hasMembership(memberships, "org", p.value)
	case principalPillarMembers:
		return hasMembership(memberships, "pillar", p.value)
	case principalTeamGroupMembers:
		return hasMembership(memberships, "team_group", p.value)
	default:
		return false
	}
}

func principalNeedsMemberships(p Principal) bool {
	switch p.kind {
	case principalTeamMembers, principalOrgMembers, principalPillarMembers, principalTeamGroupMembers:
		return true
	default:
		return false
	}
}

func hasMembership(memberships []orgdatacore.MembershipInfo, membershipType, name string) bool {
	for _, m := range memberships {
		if m.Type == membershipType && m.Name == name {
			return true
		}
	}
	return false
}

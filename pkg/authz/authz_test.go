package authz

import (
	"testing"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

type fakeResolver struct {
	employees   map[string]*orgdatacore.Employee
	memberships map[string][]orgdatacore.MembershipInfo
}

func (f *fakeResolver) GetEmployeeByGitHubID(githubID string) *orgdatacore.Employee {
	if f.employees == nil {
		return nil
	}
	return f.employees[githubID]
}

func (f *fakeResolver) GetUserMemberships(uid string) []orgdatacore.MembershipInfo {
	if f.memberships == nil {
		return nil
	}
	return f.memberships[uid]
}

func newTestResolver() *fakeResolver {
	return &fakeResolver{
		employees: map[string]*orgdatacore.Employee{
			"alice": {UID: "alice-uid", GitHubID: "alice"},
			"bob":   {UID: "bob-uid", GitHubID: "bob"},
		},
		memberships: map[string][]orgdatacore.MembershipInfo{
			"alice-uid": {
				{Name: "trt", Type: "team"},
				{Name: "ocp-eng", Type: "org"},
				{Name: "platform", Type: "pillar"},
				{Name: "ci-group", Type: "team_group"},
			},
			"bob-uid": {
				{Name: "docs", Type: "team"},
				{Name: "ocp-eng", Type: "org"},
			},
		},
	}
}

func TestIsAllowed(t *testing.T) {
	resolver := newTestResolver()

	testCases := []struct {
		name      string
		policy    *Policy
		user      string
		operation string
		resource  Resource
		expected  bool
	}{
		{
			name:      "empty policy denies everything",
			policy:    NewPolicy(),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},

		// Principal matching
		{
			name:      "User principal matches exact login",
			policy:    NewPolicy().Allow(User("alice"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "User principal is case-insensitive",
			policy:    NewPolicy().Allow(User("Alice"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "User principal does not match different user",
			policy:    NewPolicy().Allow(User("alice"), "access", Global()),
			user:      "bob",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "User principal works for unknown-to-cyborg users",
			policy:    NewPolicy().Allow(User("external"), "access", Global()),
			user:      "external",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "KnownToCyborg matches cyborg employee",
			policy:    NewPolicy().Allow(KnownToCyborg(), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "KnownToCyborg denies unknown user",
			policy:    NewPolicy().Allow(KnownToCyborg(), "access", Global()),
			user:      "external",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "TeamMembers matches team member",
			policy:    NewPolicy().Allow(TeamMembers("trt"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "TeamMembers denies non-member",
			policy:    NewPolicy().Allow(TeamMembers("trt"), "access", Global()),
			user:      "bob",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "OrgMembers matches org member",
			policy:    NewPolicy().Allow(OrgMembers("ocp-eng"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "OrgMembers denies non-member",
			policy:    NewPolicy().Allow(OrgMembers("ocp-eng"), "access", Global()),
			user:      "external",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "PillarMembers matches pillar member",
			policy:    NewPolicy().Allow(PillarMembers("platform"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "PillarMembers denies non-member",
			policy:    NewPolicy().Allow(PillarMembers("platform"), "access", Global()),
			user:      "bob",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "TeamGroupMembers matches team group member",
			policy:    NewPolicy().Allow(TeamGroupMembers("ci-group"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "TeamGroupMembers denies non-member",
			policy:    NewPolicy().Allow(TeamGroupMembers("ci-group"), "access", Global()),
			user:      "bob",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},

		// Operation matching
		{
			name:      "exact operation match",
			policy:    NewPolicy().Allow(User("alice"), "label::add::critical", Global()),
			user:      "alice",
			operation: "label::add::critical",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "hierarchical prefix: rule 'label' covers 'label::add::foo'",
			policy:    NewPolicy().Allow(User("alice"), "label", Global()),
			user:      "alice",
			operation: "label::add::foo",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "hierarchical prefix: rule 'label::add' covers 'label::add::foo'",
			policy:    NewPolicy().Allow(User("alice"), "label::add", Global()),
			user:      "alice",
			operation: "label::add::foo",
			resource:  Global(),
			expected:  true,
		},
		{
			name:      "hierarchical prefix: rule 'label::add' does not cover 'label::remove::foo'",
			policy:    NewPolicy().Allow(User("alice"), "label::add", Global()),
			user:      "alice",
			operation: "label::remove::foo",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "no partial token match: rule 'lab' does not cover 'label'",
			policy:    NewPolicy().Allow(User("alice"), "lab", Global()),
			user:      "alice",
			operation: "label",
			resource:  Global(),
			expected:  false,
		},
		{
			name:      "child operation does not cover parent: rule 'label::add' does not cover 'label'",
			policy:    NewPolicy().Allow(User("alice"), "label::add", Global()),
			user:      "alice",
			operation: "label",
			resource:  Global(),
			expected:  false,
		},

		// Resource matching
		{
			name:      "global rule matches any resource",
			policy:    NewPolicy().Allow(User("alice"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Repo("openshift", "installer"),
			expected:  true,
		},
		{
			name:      "org rule matches repo in that org",
			policy:    NewPolicy().Allow(User("alice"), "access", Org("openshift")),
			user:      "alice",
			operation: "access",
			resource:  Repo("openshift", "installer"),
			expected:  true,
		},
		{
			name:      "org rule does not match different org",
			policy:    NewPolicy().Allow(User("alice"), "access", Org("openshift")),
			user:      "alice",
			operation: "access",
			resource:  Repo("kubernetes", "kubernetes"),
			expected:  false,
		},
		{
			name:      "repo rule matches exact repo",
			policy:    NewPolicy().Allow(User("alice"), "access", Repo("openshift", "installer")),
			user:      "alice",
			operation: "access",
			resource:  Repo("openshift", "installer"),
			expected:  true,
		},
		{
			name:      "repo rule does not match different repo in same org",
			policy:    NewPolicy().Allow(User("alice"), "access", Repo("openshift", "installer")),
			user:      "alice",
			operation: "access",
			resource:  Repo("openshift", "api"),
			expected:  false,
		},
		{
			name:      "repo rule does not match org-level query",
			policy:    NewPolicy().Allow(User("alice"), "access", Repo("openshift", "installer")),
			user:      "alice",
			operation: "access",
			resource:  Org("openshift"),
			expected:  false,
		},

		// Multiple rules: any match = allow
		{
			name: "multiple rules, second matches",
			policy: NewPolicy().
				Allow(User("carol"), "access", Global()).
				Allow(User("alice"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  true,
		},
		{
			name: "multiple rules, none match",
			policy: NewPolicy().
				Allow(User("carol"), "access", Global()).
				Allow(User("dave"), "access", Global()),
			user:      "alice",
			operation: "access",
			resource:  Global(),
			expected:  false,
		},

		// Realistic scenario: coderabbit access
		{
			name:      "coderabbit access for known employee",
			policy:    NewPolicy().Allow(KnownToCyborg(), "access-coderabbit", Global()),
			user:      "alice",
			operation: "access-coderabbit",
			resource:  Repo("openshift", "installer"),
			expected:  true,
		},
		{
			name:      "coderabbit access denied for external user",
			policy:    NewPolicy().Allow(KnownToCyborg(), "access-coderabbit", Global()),
			user:      "external",
			operation: "access-coderabbit",
			resource:  Repo("openshift", "installer"),
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authz := NewAuthorizer(tc.policy, resolver)
			got := authz.IsAllowed(tc.user, tc.operation, tc.resource)
			if got != tc.expected {
				t.Errorf("IsAllowed(%q, %q, %+v) = %v, want %v", tc.user, tc.operation, tc.resource, got, tc.expected)
			}
		})
	}
}

func TestNilSafety(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		authz := NewAuthorizer(nil, newTestResolver())
		if authz.IsAllowed("alice", "access", Global()) {
			t.Error("nil policy should deny")
		}
	})

	t.Run("nil resolver", func(t *testing.T) {
		authz := NewAuthorizer(NewPolicy().Allow(KnownToCyborg(), "access", Global()), nil)
		if authz.IsAllowed("alice", "access", Global()) {
			t.Error("nil resolver should deny")
		}
	})
}

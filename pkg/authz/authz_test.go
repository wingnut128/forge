package authz

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cedar-policy/cedar-go"
)

func policiesFromCedar(t *testing.T, src string) *cedar.PolicySet {
	t.Helper()
	pl, err := cedar.NewPolicyListFromBytes("test.cedar", []byte(src))
	if err != nil {
		t.Fatalf("parsing cedar policy: %v", err)
	}
	ps := cedar.NewPolicySet()
	for i, p := range pl {
		ps.Add(cedar.PolicyID(fmt.Sprintf("test:%d", i)), p)
	}
	return ps
}

func TestCedarAuthorizer_Permit(t *testing.T) {
	ps := policiesFromCedar(t, `
		permit(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		);
	`)
	a := NewCedarAuthorizerFromPolicies(ps)

	d, err := a.IsAuthorized(
		"spiffe://remote.example.com/workload/api",
		"read-data",
		"pipeline-x",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed {
		t.Errorf("expected allowed, got denied: %s", d.Reason)
	}
}

func TestCedarAuthorizer_DenyNoMatchingPolicy(t *testing.T) {
	ps := policiesFromCedar(t, `
		permit(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		);
	`)
	a := NewCedarAuthorizerFromPolicies(ps)

	d, err := a.IsAuthorized(
		"spiffe://remote.example.com/workload/other",
		"read-data",
		"pipeline-x",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Allowed {
		t.Error("expected denied for non-matching principal")
	}
}

func TestCedarAuthorizer_ForbidOverridesPermit(t *testing.T) {
	ps := policiesFromCedar(t, `
		permit(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		);
		forbid(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		);
	`)
	a := NewCedarAuthorizerFromPolicies(ps)

	d, err := a.IsAuthorized(
		"spiffe://remote.example.com/workload/api",
		"read-data",
		"pipeline-x",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Allowed {
		t.Error("expected denied: forbid should override permit")
	}
}

func TestCedarAuthorizer_LoadFromDirectory(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "test.cedar"), []byte(`
		permit(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		);
	`), 0644)
	if err != nil {
		t.Fatalf("writing policy file: %v", err)
	}

	a, err := NewCedarAuthorizer(dir)
	if err != nil {
		t.Fatalf("loading policies: %v", err)
	}

	d, err := a.IsAuthorized(
		"spiffe://remote.example.com/workload/api",
		"read-data",
		"pipeline-x",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed {
		t.Errorf("expected allowed, got denied: %s", d.Reason)
	}
}

func TestCedarAuthorizer_AttributeBasedPolicy(t *testing.T) {
	// A policy that gates on the principal's trust_domain attribute only
	// evaluates correctly when the principal entity is registered with attrs.
	ps := policiesFromCedar(t, `
		permit(
			principal,
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		)
		when { principal.trust_domain == "remote.example.com" };
	`)
	a := NewCedarAuthorizerFromPolicies(ps)

	d, err := a.IsAuthorized("spiffe://remote.example.com/workload/api", "read-data", "pipeline-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed {
		t.Errorf("expected allow for matching trust_domain, got denied: %s", d.Reason)
	}

	// A principal from a different trust domain must not match.
	d, _ = a.IsAuthorized("spiffe://other.example.com/workload/api", "read-data", "pipeline-x")
	if d.Allowed {
		t.Error("expected deny for non-matching trust_domain")
	}
}

func TestCedarAuthorizer_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := NewCedarAuthorizer(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestCedarAuthorizer_InvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.cedar"), []byte("not valid cedar!!!"), 0644)
	_, err := NewCedarAuthorizer(dir)
	if err == nil {
		t.Fatal("expected error for invalid policy")
	}
}

func TestCedarAuthorizer_MultiplePolicyFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.cedar"), []byte(`
		permit(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
			action == Action::"read-data",
			resource == Resource::"pipeline-x"
		);
	`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.cedar"), []byte(`
		permit(
			principal == SpiffeWorkload::"spiffe://remote.example.com/workload/batch",
			action == Action::"write-data",
			resource == Resource::"pipeline-y"
		);
	`), 0644)

	a, err := NewCedarAuthorizer(dir)
	if err != nil {
		t.Fatalf("loading policies: %v", err)
	}

	// First policy
	d, _ := a.IsAuthorized("spiffe://remote.example.com/workload/api", "read-data", "pipeline-x")
	if !d.Allowed {
		t.Error("expected api/read-data/pipeline-x to be allowed")
	}

	// Second policy
	d, _ = a.IsAuthorized("spiffe://remote.example.com/workload/batch", "write-data", "pipeline-y")
	if !d.Allowed {
		t.Error("expected batch/write-data/pipeline-y to be allowed")
	}

	// Cross: should deny
	d, _ = a.IsAuthorized("spiffe://remote.example.com/workload/api", "write-data", "pipeline-y")
	if d.Allowed {
		t.Error("expected api/write-data/pipeline-y to be denied")
	}
}

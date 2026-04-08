package authz

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cedar-policy/cedar-go"
)

// Authorizer evaluates whether a principal is allowed to perform an action on a resource.
type Authorizer interface {
	IsAuthorized(principal, action, resource string) (Decision, error)
}

// Decision represents an authorization outcome.
type Decision struct {
	Allowed bool
	Reason  string
}

// CedarAuthorizer evaluates authorization using Cedar policies.
type CedarAuthorizer struct {
	policies *cedar.PolicySet
	entities cedar.EntityMap
}

// NewCedarAuthorizer loads all .cedar files from a directory into a single PolicySet.
func NewCedarAuthorizer(policyDir string) (*CedarAuthorizer, error) {
	entries, err := os.ReadDir(policyDir)
	if err != nil {
		return nil, fmt.Errorf("reading policy directory: %w", err)
	}

	ps := cedar.NewPolicySet()
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cedar") {
			continue
		}
		path := filepath.Clean(filepath.Join(policyDir, entry.Name()))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		filePolicies, err := cedar.NewPolicyListFromBytes(entry.Name(), data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		for i, p := range filePolicies {
			id := cedar.PolicyID(fmt.Sprintf("%s:%d", entry.Name(), i))
			ps.Add(id, p)
			loaded++
		}
	}

	if loaded == 0 {
		return nil, fmt.Errorf("no .cedar policy files found in %s", policyDir)
	}

	return &CedarAuthorizer{
		policies: ps,
		entities: cedar.EntityMap{},
	}, nil
}

// NewCedarAuthorizerFromPolicies creates an authorizer from a pre-built PolicySet (for testing).
func NewCedarAuthorizerFromPolicies(ps *cedar.PolicySet) *CedarAuthorizer {
	return &CedarAuthorizer{
		policies: ps,
		entities: cedar.EntityMap{},
	}
}

// IsAuthorized evaluates whether the given principal may perform the action on the resource.
func (a *CedarAuthorizer) IsAuthorized(principal, action, resource string) (Decision, error) {
	req := cedar.Request{
		Principal: cedar.NewEntityUID("SpiffeWorkload", cedar.String(principal)),
		Action:    cedar.NewEntityUID("Action", cedar.String(action)),
		Resource:  cedar.NewEntityUID("Resource", cedar.String(resource)),
		Context:   cedar.NewRecord(cedar.RecordMap{}),
	}

	decision, diagnostic := cedar.Authorize(a.policies, a.entities, req)

	if decision == cedar.Allow {
		reason := "default allow"
		if len(diagnostic.Reasons) > 0 {
			reason = fmt.Sprintf("policy %s", diagnostic.Reasons[0].PolicyID)
		}
		return Decision{Allowed: true, Reason: reason}, nil
	}

	reason := "no matching permit policy"
	if len(diagnostic.Errors) > 0 {
		reason = diagnostic.Errors[0].Message
	}
	return Decision{Allowed: false, Reason: reason}, nil
}

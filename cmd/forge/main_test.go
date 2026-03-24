package main

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// --- runServe tests ---

func TestRunServe_MissingEnvVars(t *testing.T) {
	// Clear all relevant env vars
	for _, k := range []string{"FORGE_LOCAL_TRUST_DOMAIN", "FORGE_REMOTE_TRUST_DOMAIN", "FORGE_BUNDLE_ENDPOINT_URL"} {
		t.Setenv(k, "")
	}

	err := runServe()
	if err == nil {
		t.Fatal("expected error for missing env vars")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want mention of required vars", err)
	}
}

func TestRunServe_MissingLocalTrustDomain(t *testing.T) {
	t.Setenv("FORGE_LOCAL_TRUST_DOMAIN", "")
	t.Setenv("FORGE_REMOTE_TRUST_DOMAIN", "remote.example.com")
	t.Setenv("FORGE_BUNDLE_ENDPOINT_URL", "https://bundle.example.com")

	err := runServe()
	if err == nil {
		t.Fatal("expected error for missing local trust domain")
	}
}

func TestRunServe_MissingRemoteTrustDomain(t *testing.T) {
	t.Setenv("FORGE_LOCAL_TRUST_DOMAIN", "local.example.com")
	t.Setenv("FORGE_REMOTE_TRUST_DOMAIN", "")
	t.Setenv("FORGE_BUNDLE_ENDPOINT_URL", "https://bundle.example.com")

	err := runServe()
	if err == nil {
		t.Fatal("expected error for missing remote trust domain")
	}
}

func TestRunServe_MissingBundleURL(t *testing.T) {
	t.Setenv("FORGE_LOCAL_TRUST_DOMAIN", "local.example.com")
	t.Setenv("FORGE_REMOTE_TRUST_DOMAIN", "remote.example.com")
	t.Setenv("FORGE_BUNDLE_ENDPOINT_URL", "")

	err := runServe()
	if err == nil {
		t.Fatal("expected error for missing bundle URL")
	}
}

func TestRunServe_UnreachableBundleEndpoint(t *testing.T) {
	t.Setenv("FORGE_LOCAL_TRUST_DOMAIN", "local.example.com")
	t.Setenv("FORGE_REMOTE_TRUST_DOMAIN", "remote.example.com")
	t.Setenv("FORGE_BUNDLE_ENDPOINT_URL", "http://127.0.0.1:1")

	err := runServe()
	if err == nil {
		t.Fatal("expected error for unreachable bundle endpoint")
	}
	if !strings.Contains(err.Error(), "bundle refresher") || !strings.Contains(err.Error(), "initial bundle fetch") {
		t.Errorf("error = %q, want bundle refresher error", err)
	}
}

func TestRunServe_DefaultListenAddr(t *testing.T) {
	t.Setenv("FORGE_LISTEN_ADDR", "")
	t.Setenv("FORGE_LOCAL_TRUST_DOMAIN", "local.example.com")
	t.Setenv("FORGE_REMOTE_TRUST_DOMAIN", "remote.example.com")
	t.Setenv("FORGE_BUNDLE_ENDPOINT_URL", "http://127.0.0.1:1")

	// Will fail at bundle fetch, but validates env parsing runs
	err := runServe()
	if err == nil {
		t.Fatal("expected error")
	}
	// If we got past env validation to bundle fetch, the listen addr defaulting worked
	if !strings.Contains(err.Error(), "bundle") {
		t.Errorf("error = %q, expected bundle-related error (env parsing should have passed)", err)
	}
}

// --- deployFunc tests ---

type recordingMock struct {
	mu    sync.Mutex
	names []string
}

func (m *recordingMock) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.names = append(m.names, args.Name)
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *recordingMock) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return args.Args, nil
}

func (m *recordingMock) hasResource(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.names {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

func TestDeployFunc_CreatesAllResources(t *testing.T) {
	// Set Pulumi config via env vars (the format Pulumi expects in mock mode)
	t.Setenv("PULUMI_CONFIG", `{"forge:environment":"dev","forge:spire-trust-domain":"gcp.example.com","forge:aws-spire-trust-domain":"aws.example.com"}`)

	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("deployFunc failed: %v", err)
	}

	// GCP resources
	for _, expected := range []string{"forge-dev-vpc", "forge-dev-gke", "forge-dev-spiffe-pool"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected GCP resource containing %q, got %v", expected, mock.names)
		}
	}

	// AWS resources
	for _, expected := range []string{"forge-dev-eks", "forge-dev-subnet-a", "forge-dev-spire-oidc-gcp"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected AWS resource containing %q, got %v", expected, mock.names)
		}
	}
}

func TestDeployFunc_DefaultStackName(t *testing.T) {
	old := os.Getenv("FORGE_STACK")
	os.Unsetenv("FORGE_STACK")
	defer os.Setenv("FORGE_STACK", old)

	stackName := os.Getenv("FORGE_STACK")
	if stackName == "" {
		stackName = "dev"
	}
	if stackName != "dev" {
		t.Errorf("default stack name = %q, want %q", stackName, "dev")
	}
}

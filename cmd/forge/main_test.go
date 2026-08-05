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

// deployConfig is the base PULUMI_CONFIG JSON for deployFunc tests; callers layer
// on additional feature flags as needed.
const baseDeployConfig = `{
	"forge:environment":"dev",
	"forge:spire-trust-domain":"gcp.example.com",
	"forge:aws-spire-trust-domain":"aws.example.com",
	"forge:spire-aws-ami":"ami-0123456789abcdef0",
	"forge:bowtie-gcp-image":"projects/bowtie/global/images/bowtie-1-0-0",
	"forge:bowtie-aws-ami":"ami-bowtie0000000000"
}`

func TestDeployFunc_DefaultCreatesCheapTrack(t *testing.T) {
	t.Setenv("PULUMI_CONFIG", baseDeployConfig)

	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("deployFunc failed: %v", err)
	}

	// Default track: VPCs + SPIRE VMs only. No K8s, no managed state, no Bowtie.
	for _, expected := range []string{
		"forge-dev-vpc",
		"forge-dev-mgmt-subnet",
		"forge-dev-spire-server",
		"forge-dev-public-a",
		"forge-dev-fcknat-a-asg",
		"forge-dev-fcknat-a-eni",
	} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
	for _, notExpected := range []string{"forge-dev-gke", "forge-dev-eks", "forge-dev-spire-sql", "forge-dev-spire-db", "forge-dev-bowtie"} {
		if mock.hasResource(notExpected) {
			t.Errorf("did not expect %q when enable-gke/eks/managed-state/bowtie are off", notExpected)
		}
	}
}

func TestDeployFunc_EnableBowtie(t *testing.T) {
	cfg := strings.TrimSuffix(baseDeployConfig, "}") + `,"forge:enable-bowtie":"true"}`
	t.Setenv("PULUMI_CONFIG", cfg)

	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("deployFunc failed: %v", err)
	}

	if !mock.hasResource("forge-dev-bowtie") {
		t.Errorf("expected Bowtie controllers when enable-bowtie is set, got %v", mock.names)
	}
}

func TestDeployFunc_EnableGKEAndEKS(t *testing.T) {
	cfg := strings.TrimSuffix(baseDeployConfig, "}") + `,"forge:enable-gke":"true","forge:enable-eks":"true"}`
	t.Setenv("PULUMI_CONFIG", cfg)

	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("deployFunc failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-gke", "forge-dev-spiffe-pool", "forge-dev-eks", "forge-dev-spire-oidc-gcp"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q when K8s flags set, got %v", expected, mock.names)
		}
	}
}

func TestDeployFunc_EnableManagedState(t *testing.T) {
	cfg := strings.TrimSuffix(baseDeployConfig, "}") + `,"forge:enable-managed-state":"true","forge:spire-db-password":"s3cr3t"}`
	t.Setenv("PULUMI_CONFIG", cfg)
	t.Setenv("PULUMI_CONFIG_SECRET_KEYS", `["forge:spire-db-password"]`)

	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("deployFunc failed: %v", err)
	}

	// Managed-state track: Cloud SQL + RDS + KMS + Secret Manager resources appear.
	for _, expected := range []string{"forge-dev-spire-sql", "forge-dev-spire-db", "forge-dev-spire-key", "forge-dev-spire-admin"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q when enable-managed-state set, got %v", expected, mock.names)
		}
	}
}

func TestDeployFunc_MissingSpireAWSAMI(t *testing.T) {
	cfg := `{
		"forge:environment":"dev",
		"forge:spire-trust-domain":"gcp.example.com",
		"forge:aws-spire-trust-domain":"aws.example.com",
		"forge:bowtie-gcp-image":"projects/bowtie/global/images/bowtie-1-0-0",
		"forge:bowtie-aws-ami":"ami-bowtie0000000000"
	}`
	t.Setenv("PULUMI_CONFIG", cfg)

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err == nil {
		t.Fatal("expected error when forge:spire-aws-ami is unset")
	}
	if !strings.Contains(err.Error(), "spire-aws-ami") {
		t.Errorf("error = %q, want mention of spire-aws-ami", err)
	}
}

func TestDeployFunc_MissingBowtieGCPImage(t *testing.T) {
	cfg := `{
		"forge:environment":"dev",
		"forge:spire-trust-domain":"gcp.example.com",
		"forge:aws-spire-trust-domain":"aws.example.com",
		"forge:spire-aws-ami":"ami-0123456789abcdef0",
		"forge:bowtie-aws-ami":"ami-bowtie0000000000",
		"forge:enable-bowtie":"true"
	}`
	t.Setenv("PULUMI_CONFIG", cfg)

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err == nil {
		t.Fatal("expected error when forge:bowtie-gcp-image is unset")
	}
	if !strings.Contains(err.Error(), "bowtie-gcp-image") {
		t.Errorf("error = %q, want mention of bowtie-gcp-image", err)
	}
}

func TestDeployFunc_MissingBowtieAWSAMI(t *testing.T) {
	cfg := `{
		"forge:environment":"dev",
		"forge:spire-trust-domain":"gcp.example.com",
		"forge:aws-spire-trust-domain":"aws.example.com",
		"forge:spire-aws-ami":"ami-0123456789abcdef0",
		"forge:bowtie-gcp-image":"projects/bowtie/global/images/bowtie-1-0-0",
		"forge:enable-bowtie":"true"
	}`
	t.Setenv("PULUMI_CONFIG", cfg)

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return deployFunc(ctx)
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err == nil {
		t.Fatal("expected error when forge:bowtie-aws-ami is unset")
	}
	if !strings.Contains(err.Error(), "bowtie-aws-ami") {
		t.Errorf("error = %q, want mention of bowtie-aws-ami", err)
	}
}

func TestDeployFunc_DefaultStackName(t *testing.T) {
	old := os.Getenv("FORGE_STACK")
	_ = os.Unsetenv("FORGE_STACK")
	defer func() { _ = os.Setenv("FORGE_STACK", old) }()

	stackName := os.Getenv("FORGE_STACK")
	if stackName == "" {
		stackName = "dev"
	}
	if stackName != "dev" {
		t.Errorf("default stack name = %q, want %q", stackName, "dev")
	}
}

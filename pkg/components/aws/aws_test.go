package aws

import (
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

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

// --- VPC tests ---

func TestNewVPC_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		vpc, err := NewVPC(ctx, "test-vpc", &VPCArgs{Environment: "dev", Region: "us-east-1"})
		if err != nil {
			return err
		}
		if vpc == nil {
			t.Error("expected non-nil VPC")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-vpc", "forge-dev-subnet-a", "forge-dev-subnet-b", "forge-dev-sg-internal"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

// --- EKS tests ---

func TestNewEKSCluster_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		cluster, err := NewEKSCluster(ctx, "test-eks", &EKSClusterArgs{
			Environment:  "dev",
			VpcID:        pulumi.ID("fake-vpc-id").ToIDOutput(),
			SubnetIDs:    pulumi.ToStringArray([]string{"subnet-a", "subnet-b"}).ToStringArrayOutput(),
			NodeCount:    3,
			InstanceType: "t3.medium",
		})
		if err != nil {
			return err
		}
		if cluster == nil {
			t.Error("expected non-nil EKSCluster")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-eks", "forge-dev-eks-role", "forge-dev-node-role", "forge-dev-nodegroup"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

// --- SPIRE OIDC tests ---

func TestNewSPIREOIDCProvider_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := NewSPIREOIDCProvider(ctx, "test-spire-oidc", &SPIREOIDCProviderArgs{
			Environment:         "dev",
			SPIRETrustDomain:    "forge.dev.aws.example.com",
			GCPSPIRETrustDomain: "forge.dev.gcp.example.com",
		})
		if err != nil {
			return err
		}
		if provider == nil {
			t.Error("expected non-nil SPIREOIDCProvider")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	if !mock.hasResource("forge-dev-spire-oidc-gcp") {
		t.Errorf("expected resource containing %q, got %v", "forge-dev-spire-oidc-gcp", mock.names)
	}
}

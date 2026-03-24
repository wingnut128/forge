package gcp

import (
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// recordingMock captures resource names created during a Pulumi mock run.
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

// hasResource checks if any recorded name contains the given substring.
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

// --- Network tests ---

func TestNewNetwork_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		net, err := NewNetwork(ctx, "test-network", &NetworkArgs{Environment: "dev"})
		if err != nil {
			return err
		}
		if net == nil {
			t.Error("expected non-nil Network")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-vpc", "forge-dev-subnet", "forge-dev-allow-internal"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

// --- GKECluster tests ---

func TestNewGKECluster_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		cluster, err := NewGKECluster(ctx, "test-gke", &GKEClusterArgs{
			Environment: "dev",
			NetworkID:   pulumi.ID("fake-network-id").ToIDOutput(),
			SubnetID:    pulumi.ID("fake-subnet-id").ToIDOutput(),
			NodeCount:   3,
			MachineType: "e2-standard-4",
		})
		if err != nil {
			return err
		}
		if cluster == nil {
			t.Error("expected non-nil GKECluster")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-gke", "forge-dev-nodepool"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

// --- WorkloadIdentity tests ---

func TestNewWorkloadIdentity_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		wif, err := NewWorkloadIdentity(ctx, "test-wif", &WorkloadIdentityArgs{
			Environment:      "dev",
			SPIRETrustDomain: "forge.dev.gcp.example.com",
			AWSSTrustDomain:  "forge.dev.aws.example.com",
			GKEClusterName:   pulumi.String("fake-cluster").ToStringOutput(),
		})
		if err != nil {
			return err
		}
		if wif == nil {
			t.Error("expected non-nil WorkloadIdentity")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-spiffe-pool", "forge-dev-spiffe-aws"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

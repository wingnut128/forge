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
		net, err := NewNetwork(ctx, "test-network", &NetworkArgs{Environment: "dev", Region: "us-central1"})
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
			Environment:         "dev",
			SPIRETrustDomain:    "forge.dev.gcp.example.com",
			AWSSPIRETrustDomain: "forge.dev.aws.example.com",
			GKEClusterName:      pulumi.String("fake-cluster").ToStringOutput(),
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

// --- SPIREServer tests ---

func TestNewSPIREServer_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		server, err := NewSPIREServer(ctx, "test-spire", &SPIREServerArgs{
			Environment:     "dev",
			Region:          "us-central1",
			MgmtSubnetLink:  pulumi.String("fake-subnet-link").ToStringOutput(),
			VPCID:           pulumi.ID("fake-vpc-id").ToIDOutput(),
			TrustDomain:     "forge.dev.gcp.example.com",
			PeerTrustDomain: "forge.dev.aws.example.com",
			SPIREVersion:    "1.11.2",
		})
		if err != nil {
			return err
		}
		if server == nil {
			t.Error("expected non-nil SPIREServer")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-spire-snap", "forge-dev-spire-data", "forge-dev-spire-server", "forge-dev-allow-spire"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

func TestNewSPIREServer_NilArgs(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		if _, err := NewSPIREServer(ctx, "test-spire", nil); err == nil {
			t.Error("expected error for nil args")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}
}

func TestSpireGCPStartupScript(t *testing.T) {
	tests := []struct {
		name       string
		managed    bool
		wantDBType string
	}{
		{"disk mode", false, `database_type = "sqlite3"`},
		{"managed mode", true, `database_type = "postgres"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, err := spireGCPStartupScript(&SPIREServerArgs{
				TrustDomain:      "forge.dev.gcp.example.com",
				PeerTrustDomain:  "forge.dev.aws.example.com",
				SPIREVersion:     "1.11.2",
				ManagedStateMode: tt.managed,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(script, "forge.dev.gcp.example.com") {
				t.Errorf("expected script to reference trust domain, got: %s", script)
			}
			if !strings.Contains(script, tt.wantDBType) {
				t.Errorf("expected script to contain %q, got: %s", tt.wantDBType, script)
			}
			if !strings.Contains(script, "DEV=/dev/disk/by-id/google-spire-data") {
				t.Errorf("expected script to reference GCP data disk device path, got: %s", script)
			}
		})
	}
}

func TestSpireGCPStartupScript_InvalidConfig(t *testing.T) {
	if _, err := spireGCPStartupScript(&SPIREServerArgs{}); err == nil {
		t.Error("expected error for missing trust domains")
	}
}

// --- BowtieController tests ---

func TestNewBowtieController_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		controller, err := NewBowtieController(ctx, "test-bowtie", &BowtieControllerArgs{
			Environment:    "dev",
			Region:         "us-central1",
			MgmtSubnetLink: pulumi.String("fake-subnet-link").ToStringOutput(),
			VPCID:          pulumi.ID("fake-vpc-id").ToIDOutput(),
			Image:          "projects/bowtie-public/global/images/bowtie-controller-1",
			AdminCIDRs:     []string{"203.0.113.0/24"},
		})
		if err != nil {
			return err
		}
		if controller == nil {
			t.Error("expected non-nil BowtieController")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-bowtie-ip", "forge-dev-bowtie", "forge-dev-allow-bowtie-admin", "forge-dev-allow-bowtie-mesh"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

func TestNewBowtieController_NilArgs(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		if _, err := NewBowtieController(ctx, "test-bowtie", nil); err == nil {
			t.Error("expected error for nil args")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}
}

func TestNewBowtieController_MissingImage(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewBowtieController(ctx, "test-bowtie", &BowtieControllerArgs{
			Environment:    "dev",
			Region:         "us-central1",
			MgmtSubnetLink: pulumi.String("fake-subnet-link").ToStringOutput(),
			VPCID:          pulumi.ID("fake-vpc-id").ToIDOutput(),
		})
		if err == nil {
			t.Error("expected error for missing Image")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}
}

// --- ManagedState tests ---

func TestNewManagedState_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		state, err := NewManagedState(ctx, "test-managed-state", &ManagedStateArgs{
			Environment: "dev",
			Region:      "us-central1",
		})
		if err != nil {
			return err
		}
		if state == nil {
			t.Error("expected non-nil ManagedState")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-spire-sql", "forge-dev-spire-db", "forge-dev-spire-kr", "forge-dev-spire-key", "forge-dev-spire-admin"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
}

func TestNewManagedState_NilArgs(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		if _, err := NewManagedState(ctx, "test-managed-state", nil); err == nil {
			t.Error("expected error for nil args")
		}
		return nil
	}, pulumi.WithMocks("test-project", "test-stack", &recordingMock{}))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}
}

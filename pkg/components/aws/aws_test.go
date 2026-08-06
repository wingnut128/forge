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

	for _, expected := range []string{"forge-dev-vpc", "forge-dev-subnet-a", "forge-dev-subnet-b", "forge-dev-sg-internal", "forge-dev-fcknat-a-asg"} {
		if !mock.hasResource(expected) {
			t.Errorf("expected resource containing %q, got %v", expected, mock.names)
		}
	}
	// A single NAT fleet serves both private subnets unless MultiAZNAT is set.
	if mock.hasResource("forge-dev-fcknat-b") {
		t.Errorf("did not expect a second NAT fleet when MultiAZNAT is false, got %v", mock.names)
	}
}

func TestNewVPC_MultiAZNATCreatesSecondFleet(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewVPC(ctx, "test-vpc", &VPCArgs{Environment: "dev", Region: "us-east-1", MultiAZNAT: true})
		return err
	}, pulumi.WithMocks("test-project", "test-stack", mock))
	if err != nil {
		t.Fatalf("RunErr failed: %v", err)
	}

	for _, expected := range []string{"forge-dev-fcknat-a-asg", "forge-dev-fcknat-b-asg", "forge-dev-fcknat-b-eni"} {
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

// --- SPIREServer tests ---

func TestNewSPIREServer_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		server, err := NewSPIREServer(ctx, "test-spire", &SPIREServerArgs{
			Environment:     "dev",
			Region:          "us-east-1",
			VPCID:           pulumi.ID("fake-vpc-id").ToIDOutput(),
			PrivateSubnetID: pulumi.String("fake-subnet-id").ToStringOutput(),
			InternalSGID:    pulumi.ID("fake-sg-id").ToIDOutput(),
			AMI:             "ami-fake123",
			TrustDomain:     "forge.dev.aws.example.com",
			PeerTrustDomain: "forge.dev.gcp.example.com",
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

	// The SSM profile is the only way onto this box — it has no key pair.
	for _, expected := range []string{"forge-dev-sg-spire", "forge-dev-spire-server", "forge-dev-spire-profile", "forge-dev-spire-ssm"} {
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

func TestSpireAWSUserData(t *testing.T) {
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
			userData, err := spireAWSUserData(&SPIREServerArgs{
				TrustDomain:      "forge.dev.aws.example.com",
				PeerTrustDomain:  "forge.dev.gcp.example.com",
				SPIREVersion:     "1.11.2",
				ManagedStateMode: tt.managed,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(userData, "forge.dev.aws.example.com") {
				t.Errorf("expected user data to reference trust domain, got: %s", userData)
			}
			if !strings.Contains(userData, tt.wantDBType) {
				t.Errorf("expected user data to contain %q, got: %s", tt.wantDBType, userData)
			}
			if !strings.Contains(userData, "DEV=/dev/xvdf") {
				t.Errorf("expected user data to reference AWS data volume device path, got: %s", userData)
			}
		})
	}
}

func TestSpireAWSUserData_InvalidConfig(t *testing.T) {
	if _, err := spireAWSUserData(&SPIREServerArgs{}); err == nil {
		t.Error("expected error for missing trust domains")
	}
}

// --- BowtieController tests ---

func TestNewBowtieController_CreatesResources(t *testing.T) {
	mock := &recordingMock{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		controller, err := NewBowtieController(ctx, "test-bowtie", &BowtieControllerArgs{
			Environment:    "dev",
			Region:         "us-east-1",
			VPCID:          pulumi.ID("fake-vpc-id").ToIDOutput(),
			PublicSubnetID: pulumi.String("fake-subnet-id").ToStringOutput(),
			AMI:            "ami-fake123",
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

	for _, expected := range []string{"forge-dev-sg-bowtie", "forge-dev-eip-bowtie", "forge-dev-bowtie", "forge-dev-eip-bowtie-assoc"} {
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

func TestNewBowtieController_MissingAMI(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewBowtieController(ctx, "test-bowtie", &BowtieControllerArgs{
			Environment:    "dev",
			Region:         "us-east-1",
			VPCID:          pulumi.ID("fake-vpc-id").ToIDOutput(),
			PublicSubnetID: pulumi.String("fake-subnet-id").ToStringOutput(),
		})
		if err == nil {
			t.Error("expected error for missing AMI")
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
			Environment:      "dev",
			PrivateSubnetIDs: pulumi.ToStringArray([]string{"subnet-a", "subnet-b"}).ToStringArrayOutput(),
			InternalSGID:     pulumi.ID("fake-sg-id").ToIDOutput(),
			DBPassword:       pulumi.String("fake-password"),
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

	for _, expected := range []string{"forge-dev-spire-sng", "forge-dev-spire-db", "forge-dev-spire-key", "forge-dev-spire-admin"} {
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

func TestRenderForgeServeScript(t *testing.T) {
	got, err := renderForgeServeScript(forgeServeArgs{
		LocalTrustDomain:  "forge.dev.aws",
		RemoteTrustDomain: "forge.dev.gcp",
		BundleEndpointURL: "https://10.0.16.10:8443",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"FORGE_LOCAL_TRUST_DOMAIN=forge.dev.aws",
		"FORGE_REMOTE_TRUST_DOMAIN=forge.dev.gcp",
		"FORGE_BUNDLE_ENDPOINT_URL=https://10.0.16.10:8443",
		"ExecStart=/usr/local/bin/forge serve",
		"--branch main",
		// The initial bundle fetch is fatal, so it must retry until bootstrap.
		"Restart=always",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("forge serve script missing %q:\n%s", want, got)
		}
	}
	// Authorization stays opt-in; no policy dir should be configured.
	if strings.Contains(got, "FORGE_POLICY_DIR") {
		t.Errorf("forge serve should not enable authz by default:\n%s", got)
	}
}

func TestRenderForgeServeScript_HonorsRepoRef(t *testing.T) {
	got, err := renderForgeServeScript(forgeServeArgs{
		LocalTrustDomain:  "forge.dev.aws",
		RemoteTrustDomain: "forge.dev.gcp",
		BundleEndpointURL: "https://10.0.16.10:8443",
		RepoRef:           "v0.2.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "--branch v0.2.0") {
		t.Errorf("repo ref not honored:\n%s", got)
	}
}

func TestRenderForgeServeScript_RequiresTrustDomains(t *testing.T) {
	for _, tc := range []struct {
		name string
		args forgeServeArgs
	}{
		{"no local td", forgeServeArgs{RemoteTrustDomain: "b", BundleEndpointURL: "https://x:8443"}},
		{"no remote td", forgeServeArgs{LocalTrustDomain: "a", BundleEndpointURL: "https://x:8443"}},
		{"no bundle url", forgeServeArgs{LocalTrustDomain: "a", RemoteTrustDomain: "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := renderForgeServeScript(tc.args); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

// The AWS user data must carry both the SPIRE server and the validator.
func TestSpireAWSUserData_IncludesForgeServe(t *testing.T) {
	got, err := spireAWSUserData(&SPIREServerArgs{
		Environment:     "dev",
		AMI:             "ami-fake",
		TrustDomain:     "forge.dev.aws",
		PeerTrustDomain: "forge.dev.gcp",
		SPIREVersion:    "1.11.2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"spire-server.service", "forge-serve.service", gcpSPIREServerPrivateIP} {
		if !strings.Contains(got, want) {
			t.Errorf("user data missing %q", want)
		}
	}
}

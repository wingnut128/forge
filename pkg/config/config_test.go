package config

import "testing"

// baseInput returns a minimally valid ConfigInput for tests to tweak.
func baseInput() ConfigInput {
	return ConfigInput{
		Environment:         "dev",
		SPIRETrustDomain:    "td.example.com",
		AWSSPIRETrustDomain: "aws.example.com",
	}
}

func mustConfig(t *testing.T, in ConfigInput) *ForgeConfig {
	t.Helper()
	cfg, err := NewForgeConfig(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return cfg
}

func TestNewForgeConfig_AllFieldsSet(t *testing.T) {
	cfg := mustConfig(t, ConfigInput{
		Environment:         "prod",
		SPIRETrustDomain:    "forge.gcp.example.com",
		AWSSPIRETrustDomain: "forge.aws.example.com",
		GKENodeCount:        5,
		GKEMachineType:      "n2-standard-8",
	})

	if cfg.Environment != "prod" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "prod")
	}
	if cfg.SPIRETrustDomain != "forge.gcp.example.com" {
		t.Errorf("SPIRETrustDomain = %q", cfg.SPIRETrustDomain)
	}
	if cfg.AWSSPIRETrustDomain != "forge.aws.example.com" {
		t.Errorf("AWSSPIRETrustDomain = %q", cfg.AWSSPIRETrustDomain)
	}
	if cfg.GKENodeCount != 5 {
		t.Errorf("GKENodeCount = %d, want 5", cfg.GKENodeCount)
	}
	if cfg.GKEMachineType != "n2-standard-8" {
		t.Errorf("GKEMachineType = %q", cfg.GKEMachineType)
	}
}

func TestNewForgeConfig_Defaults(t *testing.T) {
	cfg := mustConfig(t, baseInput())
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
	if cfg.GKEMachineType != DefaultGKEMachineType {
		t.Errorf("GKEMachineType = %q, want default %q", cfg.GKEMachineType, DefaultGKEMachineType)
	}
	if cfg.EKSNodeCount != DefaultEKSNodeCount {
		t.Errorf("EKSNodeCount = %d", cfg.EKSNodeCount)
	}
	if cfg.EKSInstanceType != DefaultEKSInstanceType {
		t.Errorf("EKSInstanceType = %q", cfg.EKSInstanceType)
	}
	if cfg.GCPRegion != DefaultGCPRegion {
		t.Errorf("GCPRegion = %q", cfg.GCPRegion)
	}
	if cfg.AWSRegion != DefaultAWSRegion {
		t.Errorf("AWSRegion = %q", cfg.AWSRegion)
	}
	if cfg.SPIREServerVersion != DefaultSPIREServerVersion {
		t.Errorf("SPIREServerVersion = %q", cfg.SPIREServerVersion)
	}
	if cfg.EnableGKE || cfg.EnableEKS || cfg.EnableManagedState || cfg.EnableBowtie {
		t.Errorf("enable flags should default false: gke=%v eks=%v managed=%v bowtie=%v", cfg.EnableGKE, cfg.EnableEKS, cfg.EnableManagedState, cfg.EnableBowtie)
	}
}

func TestNewForgeConfig_NegativeNodeCountFallsBack(t *testing.T) {
	in := baseInput()
	in.GKENodeCount = -1
	in.EKSNodeCount = -1
	cfg := mustConfig(t, in)
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d", cfg.GKENodeCount)
	}
	if cfg.EKSNodeCount != DefaultEKSNodeCount {
		t.Errorf("EKSNodeCount = %d", cfg.EKSNodeCount)
	}
}

func TestNewForgeConfig_EKSFieldsSet(t *testing.T) {
	in := baseInput()
	in.EKSNodeCount = 5
	in.EKSInstanceType = "m5.xlarge"
	cfg := mustConfig(t, in)
	if cfg.EKSNodeCount != 5 {
		t.Errorf("EKSNodeCount = %d", cfg.EKSNodeCount)
	}
	if cfg.EKSInstanceType != "m5.xlarge" {
		t.Errorf("EKSInstanceType = %q", cfg.EKSInstanceType)
	}
}

func TestNewForgeConfig_EnableFlagsPassthrough(t *testing.T) {
	in := baseInput()
	in.EnableGKE = true
	in.EnableEKS = true
	in.EnableManagedState = true
	in.EnableMultiAZNAT = true
	in.EnableBowtie = true
	cfg := mustConfig(t, in)
	if !cfg.EnableGKE || !cfg.EnableEKS || !cfg.EnableManagedState || !cfg.EnableMultiAZNAT || !cfg.EnableBowtie {
		t.Errorf("enable flags not passed through: %+v", cfg)
	}
}

func TestNewForgeConfig_BowtieFields(t *testing.T) {
	in := baseInput()
	in.BowtieAdminCIDRs = []string{"10.0.0.0/8", "203.0.113.0/24"}
	in.BowtieGCPImage = "projects/bowtie-public/global/images/bowtie-controller-1-0-0"
	in.BowtieAWSAMI = "ami-0123456789abcdef0"
	cfg := mustConfig(t, in)
	if len(cfg.BowtieAdminCIDRs) != 2 {
		t.Errorf("BowtieAdminCIDRs len = %d", len(cfg.BowtieAdminCIDRs))
	}
	if cfg.BowtieGCPImage == "" || cfg.BowtieAWSAMI == "" {
		t.Errorf("bowtie image/AMI not passed through")
	}
}

func TestNewForgeConfig_InvalidBowtieCIDR(t *testing.T) {
	in := baseInput()
	in.BowtieAdminCIDRs = []string{"not-a-cidr"}
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestNewForgeConfig_SPIREVersionPassthrough(t *testing.T) {
	in := baseInput()
	in.SPIREServerVersion = "1.10.0"
	cfg := mustConfig(t, in)
	if cfg.SPIREServerVersion != "1.10.0" {
		t.Errorf("SPIREServerVersion = %q", cfg.SPIREServerVersion)
	}
}

// Validation tests

func TestNewForgeConfig_EmptyEnvironment(t *testing.T) {
	in := baseInput()
	in.Environment = ""
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for empty environment")
	}
}

func TestNewForgeConfig_EnvironmentSpecialChars(t *testing.T) {
	in := baseInput()
	in.Environment = "prod!@#"
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for environment with special chars")
	}
}

func TestNewForgeConfig_EnvironmentUppercase(t *testing.T) {
	in := baseInput()
	in.Environment = "Prod"
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for uppercase environment")
	}
}

func TestNewForgeConfig_EnvironmentWithHyphens(t *testing.T) {
	in := baseInput()
	in.Environment = "dev-east"
	cfg := mustConfig(t, in)
	if cfg.Environment != "dev-east" {
		t.Errorf("Environment = %q", cfg.Environment)
	}
}

func TestNewForgeConfig_EmptyTrustDomain(t *testing.T) {
	in := baseInput()
	in.SPIRETrustDomain = ""
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for empty spire-trust-domain")
	}
}

func TestNewForgeConfig_EmptyAWSTrustDomain(t *testing.T) {
	in := baseInput()
	in.AWSSPIRETrustDomain = ""
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for empty aws-spire-trust-domain")
	}
}

func TestNewForgeConfig_InvalidTrustDomain(t *testing.T) {
	in := baseInput()
	in.SPIRETrustDomain = "not a domain!"
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for invalid trust domain")
	}
}

func TestNewForgeConfig_TrustDomainWithUppercase(t *testing.T) {
	in := baseInput()
	in.SPIRETrustDomain = "Forge.Example.Com"
	if _, err := NewForgeConfig(in); err == nil {
		t.Fatal("expected error for uppercase trust domain")
	}
}

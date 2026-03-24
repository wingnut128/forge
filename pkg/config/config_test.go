package config

import "testing"

// validConfig is a helper that calls NewForgeConfig and fails the test on error.
func validConfig(t *testing.T, environment, spireTD, awsTD string, nodeCount int, machineType string) *ForgeConfig {
	t.Helper()
	cfg, err := NewForgeConfig(environment, spireTD, awsTD, nodeCount, machineType)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return cfg
}

func TestNewForgeConfig_AllFieldsSet(t *testing.T) {
	cfg := validConfig(t, "prod", "forge.gcp.example.com", "forge.aws.example.com", 5, "n2-standard-8")

	if cfg.Environment != "prod" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "prod")
	}
	if cfg.SPIRETrustDomain != "forge.gcp.example.com" {
		t.Errorf("SPIRETrustDomain = %q, want %q", cfg.SPIRETrustDomain, "forge.gcp.example.com")
	}
	if cfg.AWSSPIRETrustDomain != "forge.aws.example.com" {
		t.Errorf("AWSSPIRETrustDomain = %q, want %q", cfg.AWSSPIRETrustDomain, "forge.aws.example.com")
	}
	if cfg.GKENodeCount != 5 {
		t.Errorf("GKENodeCount = %d, want %d", cfg.GKENodeCount, 5)
	}
	if cfg.GKEMachineType != "n2-standard-8" {
		t.Errorf("GKEMachineType = %q, want %q", cfg.GKEMachineType, "n2-standard-8")
	}
}

func TestNewForgeConfig_DefaultNodeCount(t *testing.T) {
	cfg := validConfig(t, "dev", "td.example.com", "aws.example.com", 0, "e2-medium")
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
}

func TestNewForgeConfig_NegativeNodeCount(t *testing.T) {
	cfg := validConfig(t, "dev", "td.example.com", "aws.example.com", -1, "e2-medium")
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
}

func TestNewForgeConfig_DefaultMachineType(t *testing.T) {
	cfg := validConfig(t, "dev", "td.example.com", "aws.example.com", 3, "")
	if cfg.GKEMachineType != DefaultGKEMachineType {
		t.Errorf("GKEMachineType = %q, want default %q", cfg.GKEMachineType, DefaultGKEMachineType)
	}
}

func TestNewForgeConfig_BothDefaults(t *testing.T) {
	cfg := validConfig(t, "dev", "td.example.com", "aws.example.com", 0, "")
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
	if cfg.GKEMachineType != DefaultGKEMachineType {
		t.Errorf("GKEMachineType = %q, want default %q", cfg.GKEMachineType, DefaultGKEMachineType)
	}
}

func TestNewForgeConfig_PassthroughFields(t *testing.T) {
	cfg := validConfig(t, "staging", "gcp.example.com", "aws.example.com", 1, "e2-micro")
	if cfg.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "staging")
	}
	if cfg.SPIRETrustDomain != "gcp.example.com" {
		t.Errorf("SPIRETrustDomain = %q, want %q", cfg.SPIRETrustDomain, "gcp.example.com")
	}
	if cfg.AWSSPIRETrustDomain != "aws.example.com" {
		t.Errorf("AWSSPIRETrustDomain = %q, want %q", cfg.AWSSPIRETrustDomain, "aws.example.com")
	}
}

// Validation tests

func TestNewForgeConfig_EmptyEnvironment(t *testing.T) {
	_, err := NewForgeConfig("", "td.example.com", "aws.example.com", 3, "")
	if err == nil {
		t.Fatal("expected error for empty environment")
	}
}

func TestNewForgeConfig_EnvironmentSpecialChars(t *testing.T) {
	_, err := NewForgeConfig("prod!@#", "td.example.com", "aws.example.com", 3, "")
	if err == nil {
		t.Fatal("expected error for environment with special chars")
	}
}

func TestNewForgeConfig_EnvironmentUppercase(t *testing.T) {
	_, err := NewForgeConfig("Prod", "td.example.com", "aws.example.com", 3, "")
	if err == nil {
		t.Fatal("expected error for uppercase environment")
	}
}

func TestNewForgeConfig_EnvironmentWithHyphens(t *testing.T) {
	cfg := validConfig(t, "dev-east", "td.example.com", "aws.example.com", 3, "")
	if cfg.Environment != "dev-east" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "dev-east")
	}
}

func TestNewForgeConfig_SingleCharEnvironment(t *testing.T) {
	cfg := validConfig(t, "d", "td.example.com", "aws.example.com", 3, "")
	if cfg.Environment != "d" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "d")
	}
}

func TestNewForgeConfig_EmptyTrustDomain(t *testing.T) {
	_, err := NewForgeConfig("dev", "", "aws.example.com", 3, "")
	if err == nil {
		t.Fatal("expected error for empty spire-trust-domain")
	}
}

func TestNewForgeConfig_EmptyAWSTrustDomain(t *testing.T) {
	_, err := NewForgeConfig("dev", "td.example.com", "", 3, "")
	if err == nil {
		t.Fatal("expected error for empty aws-spire-trust-domain")
	}
}

func TestNewForgeConfig_InvalidTrustDomain(t *testing.T) {
	_, err := NewForgeConfig("dev", "not a domain!", "aws.example.com", 3, "")
	if err == nil {
		t.Fatal("expected error for invalid trust domain")
	}
}

func TestNewForgeConfig_TrustDomainWithUppercase(t *testing.T) {
	_, err := NewForgeConfig("dev", "Forge.Example.Com", "aws.example.com", 3, "")
	if err == nil {
		t.Fatal("expected error for uppercase trust domain")
	}
}

func TestNewForgeConfig_ValidTrustDomains(t *testing.T) {
	cfg := validConfig(t, "dev", "forge.dev.gcp.example.com", "forge.dev.aws.example.com", 3, "")
	if cfg.SPIRETrustDomain != "forge.dev.gcp.example.com" {
		t.Errorf("SPIRETrustDomain = %q, want %q", cfg.SPIRETrustDomain, "forge.dev.gcp.example.com")
	}
	if cfg.AWSSPIRETrustDomain != "forge.dev.aws.example.com" {
		t.Errorf("AWSSPIRETrustDomain = %q, want %q", cfg.AWSSPIRETrustDomain, "forge.dev.aws.example.com")
	}
}

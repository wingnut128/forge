package config

import "testing"

func TestNewForgeConfig_AllFieldsSet(t *testing.T) {
	cfg := NewForgeConfig("prod", "forge.gcp.example.com", "forge.aws.example.com", 5, "n2-standard-8")

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
	cfg := NewForgeConfig("dev", "td", "aws-td", 0, "e2-medium")
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
}

func TestNewForgeConfig_NegativeNodeCount(t *testing.T) {
	cfg := NewForgeConfig("dev", "td", "aws-td", -1, "e2-medium")
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
}

func TestNewForgeConfig_DefaultMachineType(t *testing.T) {
	cfg := NewForgeConfig("dev", "td", "aws-td", 3, "")
	if cfg.GKEMachineType != DefaultGKEMachineType {
		t.Errorf("GKEMachineType = %q, want default %q", cfg.GKEMachineType, DefaultGKEMachineType)
	}
}

func TestNewForgeConfig_BothDefaults(t *testing.T) {
	cfg := NewForgeConfig("dev", "td", "aws-td", 0, "")
	if cfg.GKENodeCount != DefaultGKENodeCount {
		t.Errorf("GKENodeCount = %d, want default %d", cfg.GKENodeCount, DefaultGKENodeCount)
	}
	if cfg.GKEMachineType != DefaultGKEMachineType {
		t.Errorf("GKEMachineType = %q, want default %q", cfg.GKEMachineType, DefaultGKEMachineType)
	}
}

func TestNewForgeConfig_PassthroughFields(t *testing.T) {
	cfg := NewForgeConfig("staging", "gcp.example.com", "aws.example.com", 1, "e2-micro")
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

package config

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// ForgeConfig holds all stack configuration values.
type ForgeConfig struct {
	Environment       string
	GKENodeCount      int
	GKEMachineType    string
	SPIRETrustDomain  string
	AWSSPIRETrustDomain string
}

// DefaultGKENodeCount is used when gke-node-count is not set or zero.
const DefaultGKENodeCount = 3

// DefaultGKEMachineType is used when gke-machine-type is not set.
const DefaultGKEMachineType = "e2-standard-4"

// Load reads configuration from the active Pulumi stack.
func Load(ctx *pulumi.Context) *ForgeConfig {
	cfg := config.New(ctx, "forge")
	return NewForgeConfig(
		cfg.Require("environment"),
		cfg.Require("spire-trust-domain"),
		cfg.Require("aws-spire-trust-domain"),
		cfg.GetInt("gke-node-count"),
		cfg.Get("gke-machine-type"),
	)
}

// NewForgeConfig creates a ForgeConfig with defaults applied for optional fields.
func NewForgeConfig(environment, spireTrustDomain, awsSPIRETrustDomain string, gkeNodeCount int, gkeMachineType string) *ForgeConfig {
	if gkeNodeCount <= 0 {
		gkeNodeCount = DefaultGKENodeCount
	}
	if gkeMachineType == "" {
		gkeMachineType = DefaultGKEMachineType
	}
	return &ForgeConfig{
		Environment:         environment,
		GKENodeCount:        gkeNodeCount,
		GKEMachineType:      gkeMachineType,
		SPIRETrustDomain:    spireTrustDomain,
		AWSSPIRETrustDomain: awsSPIRETrustDomain,
	}
}

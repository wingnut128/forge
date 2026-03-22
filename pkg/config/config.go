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

// Load reads configuration from the active Pulumi stack.
func Load(ctx *pulumi.Context) *ForgeConfig {
	cfg := config.New(ctx, "forge")

	nodeCount := cfg.GetInt("gke-node-count")
	if nodeCount == 0 {
		nodeCount = 3
	}

	machineType := cfg.Get("gke-machine-type")
	if machineType == "" {
		machineType = "e2-standard-4"
	}

	return &ForgeConfig{
		Environment:         cfg.Require("environment"),
		GKENodeCount:        nodeCount,
		GKEMachineType:      machineType,
		SPIRETrustDomain:    cfg.Require("spire-trust-domain"),
		AWSSPIRETrustDomain: cfg.Require("aws-spire-trust-domain"),
	}
}

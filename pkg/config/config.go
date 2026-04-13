package config

import (
	"fmt"
	"regexp"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// validEnvironment matches lowercase alphanumeric names with optional hyphens (no leading/trailing).
var validEnvironment = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

// validTrustDomain matches DNS-like names per the SPIFFE spec (lowercase labels separated by dots).
var validTrustDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]*[a-z0-9])?)*$`)

// ForgeConfig holds all stack configuration values.
type ForgeConfig struct {
	Environment         string
	GCPRegion           string
	AWSRegion           string
	GKENodeCount        int
	GKEMachineType      string
	EKSNodeCount        int
	EKSInstanceType     string
	SPIRETrustDomain    string
	AWSSPIRETrustDomain string
}

// DefaultGKENodeCount is used when gke-node-count is not set or zero.
const DefaultGKENodeCount = 3

// DefaultGKEMachineType is used when gke-machine-type is not set.
const DefaultGKEMachineType = "e2-standard-4"

// DefaultEKSNodeCount is used when eks-node-count is not set or zero.
const DefaultEKSNodeCount = 3

// DefaultEKSInstanceType is used when eks-instance-type is not set.
const DefaultEKSInstanceType = "t3.medium"

const DefaultGCPRegion = "us-central1"

const DefaultAWSRegion = "us-east-1"

// Load reads configuration from the active Pulumi stack.
func Load(ctx *pulumi.Context) (*ForgeConfig, error) {
	cfg := config.New(ctx, "forge")
	return NewForgeConfig(
		cfg.Require("environment"),
		cfg.Require("spire-trust-domain"),
		cfg.Require("aws-spire-trust-domain"),
		cfg.Get("gcp-region"),
		cfg.Get("aws-region"),
		cfg.GetInt("gke-node-count"),
		cfg.Get("gke-machine-type"),
		cfg.GetInt("eks-node-count"),
		cfg.Get("eks-instance-type"),
	)
}

// NewForgeConfig creates a ForgeConfig with validated inputs and defaults for optional fields.
func NewForgeConfig(environment, spireTrustDomain, awsSPIRETrustDomain, gcpRegion, awsRegion string, gkeNodeCount int, gkeMachineType string, eksNodeCount int, eksInstanceType string) (*ForgeConfig, error) {
	if environment == "" {
		return nil, fmt.Errorf("environment must not be empty")
	}
	if !validEnvironment.MatchString(environment) {
		return nil, fmt.Errorf("environment %q must be lowercase alphanumeric with optional hyphens", environment)
	}
	if err := validateTrustDomain("spire-trust-domain", spireTrustDomain); err != nil {
		return nil, err
	}
	if err := validateTrustDomain("aws-spire-trust-domain", awsSPIRETrustDomain); err != nil {
		return nil, err
	}
	if gkeNodeCount <= 0 {
		gkeNodeCount = DefaultGKENodeCount
	}
	if gkeMachineType == "" {
		gkeMachineType = DefaultGKEMachineType
	}
	if eksNodeCount <= 0 {
		eksNodeCount = DefaultEKSNodeCount
	}
	if eksInstanceType == "" {
		eksInstanceType = DefaultEKSInstanceType
	}
	if gcpRegion == "" {
		gcpRegion = DefaultGCPRegion
	}
	if awsRegion == "" {
		awsRegion = DefaultAWSRegion
	}
	return &ForgeConfig{
		Environment:         environment,
		GCPRegion:           gcpRegion,
		AWSRegion:           awsRegion,
		GKENodeCount:        gkeNodeCount,
		GKEMachineType:      gkeMachineType,
		EKSNodeCount:        eksNodeCount,
		EKSInstanceType:     eksInstanceType,
		SPIRETrustDomain:    spireTrustDomain,
		AWSSPIRETrustDomain: awsSPIRETrustDomain,
	}, nil
}

func validateTrustDomain(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if !validTrustDomain.MatchString(value) {
		return fmt.Errorf("%s %q must be a valid DNS-like name (lowercase, dots, hyphens)", field, value)
	}
	return nil
}

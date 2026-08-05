package config

import (
	"fmt"
	"regexp"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func defaultString(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func defaultInt(val, def int) int {
	if val <= 0 {
		return def
	}
	return val
}

// validEnvironment matches lowercase alphanumeric names with optional hyphens (no leading/trailing).
var validEnvironment = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

// validTrustDomain matches DNS-like names per the SPIFFE spec (lowercase labels separated by dots).
var validTrustDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]*[a-z0-9])?)*$`)

// validCIDR matches IPv4 CIDR blocks.
var validCIDR = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}/\d{1,2}$`)

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

	EnableGKE          bool
	EnableEKS          bool
	EnableManagedState bool
	EnableMultiAZNAT   bool
	EnableBowtie       bool

	BowtieAdminCIDRs []string
	BowtieGCPImage   string
	BowtieAWSAMI     string

	SPIREServerVersion string
}

const (
	DefaultGKENodeCount       = 3
	DefaultGKEMachineType     = "e2-standard-4"
	DefaultEKSNodeCount       = 3
	DefaultEKSInstanceType    = "t3.medium"
	DefaultGCPRegion          = "us-central1"
	DefaultAWSRegion          = "us-east-1"
	DefaultSPIREServerVersion = "1.11.2"
)

// ConfigInput is the full set of raw inputs accepted by NewForgeConfig.
type ConfigInput struct {
	Environment         string
	SPIRETrustDomain    string
	AWSSPIRETrustDomain string
	GCPRegion           string
	AWSRegion           string
	GKENodeCount        int
	GKEMachineType      string
	EKSNodeCount        int
	EKSInstanceType     string

	EnableGKE          bool
	EnableEKS          bool
	EnableManagedState bool
	EnableMultiAZNAT   bool
	EnableBowtie       bool

	BowtieAdminCIDRs []string
	BowtieGCPImage   string
	BowtieAWSAMI     string

	SPIREServerVersion string
}

// Load reads configuration from the active Pulumi stack.
func Load(ctx *pulumi.Context) (*ForgeConfig, error) {
	cfg := config.New(ctx, "forge")

	var adminCIDRs []string
	if err := cfg.GetObject("bowtie-admin-cidrs", &adminCIDRs); err != nil {
		// GetObject errors when the key is both present and malformed (not a JSON array).
		// An unset key returns nil without error — we leave the empty slice.
		return nil, fmt.Errorf("bowtie-admin-cidrs: %w", err)
	}

	return NewForgeConfig(ConfigInput{
		Environment:         cfg.Require("environment"),
		SPIRETrustDomain:    cfg.Require("spire-trust-domain"),
		AWSSPIRETrustDomain: cfg.Require("aws-spire-trust-domain"),
		GCPRegion:           cfg.Get("gcp-region"),
		AWSRegion:           cfg.Get("aws-region"),
		GKENodeCount:        cfg.GetInt("gke-node-count"),
		GKEMachineType:      cfg.Get("gke-machine-type"),
		EKSNodeCount:        cfg.GetInt("eks-node-count"),
		EKSInstanceType:     cfg.Get("eks-instance-type"),
		EnableGKE:           cfg.GetBool("enable-gke"),
		EnableEKS:           cfg.GetBool("enable-eks"),
		EnableManagedState:  cfg.GetBool("enable-managed-state"),
		EnableMultiAZNAT:    cfg.GetBool("enable-multi-az-nat"),
		EnableBowtie:        cfg.GetBool("enable-bowtie"),
		BowtieAdminCIDRs:    adminCIDRs,
		BowtieGCPImage:      cfg.Get("bowtie-gcp-image"),
		BowtieAWSAMI:        cfg.Get("bowtie-aws-ami"),
		SPIREServerVersion:  cfg.Get("spire-server-version"),
	})
}

// NewForgeConfig creates a ForgeConfig with validated inputs and defaults for optional fields.
func NewForgeConfig(in ConfigInput) (*ForgeConfig, error) {
	if in.Environment == "" {
		return nil, fmt.Errorf("environment must not be empty")
	}
	if !validEnvironment.MatchString(in.Environment) {
		return nil, fmt.Errorf("environment %q must be lowercase alphanumeric with optional hyphens", in.Environment)
	}
	if err := validateTrustDomain("spire-trust-domain", in.SPIRETrustDomain); err != nil {
		return nil, err
	}
	if err := validateTrustDomain("aws-spire-trust-domain", in.AWSSPIRETrustDomain); err != nil {
		return nil, err
	}
	for _, c := range in.BowtieAdminCIDRs {
		if !validCIDR.MatchString(c) {
			return nil, fmt.Errorf("bowtie-admin-cidrs entry %q is not a valid IPv4 CIDR", c)
		}
	}

	gkeNodeCount := defaultInt(in.GKENodeCount, DefaultGKENodeCount)
	gkeMachineType := defaultString(in.GKEMachineType, DefaultGKEMachineType)
	eksNodeCount := defaultInt(in.EKSNodeCount, DefaultEKSNodeCount)
	eksInstanceType := defaultString(in.EKSInstanceType, DefaultEKSInstanceType)
	gcpRegion := defaultString(in.GCPRegion, DefaultGCPRegion)
	awsRegion := defaultString(in.AWSRegion, DefaultAWSRegion)
	spireVersion := defaultString(in.SPIREServerVersion, DefaultSPIREServerVersion)

	return &ForgeConfig{
		Environment:         in.Environment,
		GCPRegion:           gcpRegion,
		AWSRegion:           awsRegion,
		GKENodeCount:        gkeNodeCount,
		GKEMachineType:      gkeMachineType,
		EKSNodeCount:        eksNodeCount,
		EKSInstanceType:     eksInstanceType,
		SPIRETrustDomain:    in.SPIRETrustDomain,
		AWSSPIRETrustDomain: in.AWSSPIRETrustDomain,
		EnableGKE:           in.EnableGKE,
		EnableEKS:           in.EnableEKS,
		EnableManagedState:  in.EnableManagedState,
		EnableMultiAZNAT:    in.EnableMultiAZNAT,
		EnableBowtie:        in.EnableBowtie,
		BowtieAdminCIDRs:    in.BowtieAdminCIDRs,
		BowtieGCPImage:      in.BowtieGCPImage,
		BowtieAWSAMI:        in.BowtieAWSAMI,
		SPIREServerVersion:  spireVersion,
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

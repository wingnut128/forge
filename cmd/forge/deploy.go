package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	awscomp "github.com/wingnut128/forge/pkg/components/aws"
	"github.com/wingnut128/forge/pkg/components/gcp"
	forgeconfig "github.com/wingnut128/forge/pkg/config"
	"github.com/wingnut128/forge/pkg/policies"
)

// deployFunc is the inline Pulumi program invoked by the Automation API.
func deployFunc(ctx *pulumi.Context) error {
	cfg, err := forgeconfig.Load(ctx)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if err := policyCheckPhase(ctx, cfg); err != nil {
		return err
	}

	network, err := gcpFoundationPhase(ctx, cfg)
	if err != nil {
		return err
	}

	awsVPC, err := awsFoundationPhase(ctx, cfg)
	if err != nil {
		return err
	}

	if err := optionalGKEDomainPhase(ctx, cfg, network); err != nil {
		return err
	}

	if err := optionalEKSDomainPhase(ctx, cfg, awsVPC); err != nil {
		return err
	}

	if err := optionalManagedStatePhase(ctx, cfg, awsVPC); err != nil {
		return err
	}

	if err := spireServerPhase(ctx, cfg, network, awsVPC); err != nil {
		return err
	}

	if err := bowtieControllerPhase(ctx, cfg, network, awsVPC); err != nil {
		return err
	}

	return nil
}

func policyCheckPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig) error {
	result := &policies.Result{}

	policies.CheckNetwork(policies.NetworkPolicyInput{
		Environment:         cfg.Environment,
		ResourceName:        fmt.Sprintf("forge-%s-vpc", cfg.Environment),
		CustomSubnetMode:    true,
		PrivateGoogleAccess: true,
	}, result)

	if cfg.EnableGKE {
		policies.CheckGKE(policies.GKEPolicyInput{
			Environment:       cfg.Environment,
			ResourceName:      fmt.Sprintf("forge-%s-gke", cfg.Environment),
			PrivateCluster:    true,
			WorkloadIdentity:  true,
			BinaryAuthEnabled: true,
			NetworkPolicy:     true,
			SecureBoot:        true,
			IntegrityMonitor:  true,
			AutoRepair:        true,
			AutoUpgrade:       true,
		}, result)

		policies.CheckWorkloadIdentity(policies.WorkloadIdentityPolicyInput{
			Environment:        cfg.Environment,
			ResourceName:       fmt.Sprintf("forge-%s-spiffe-pool", cfg.Environment),
			AttributeCondition: fmt.Sprintf("assertion.sub.startsWith('spiffe://%s/')", cfg.AWSSPIRETrustDomain),
			HasAudiences:       cfg.SPIRETrustDomain != "",
		}, result)
	}

	policies.CheckAWSVPC(policies.AWSVPCPolicyInput{
		Environment:    cfg.Environment,
		ResourceName:   fmt.Sprintf("forge-%s-vpc", cfg.Environment),
		CustomVPC:      true,
		MultiAZ:        true,
		PrivateSubnets: true,
	}, result)

	if cfg.EnableEKS {
		policies.CheckEKS(policies.EKSPolicyInput{
			Environment:      cfg.Environment,
			ResourceName:     fmt.Sprintf("forge-%s-eks", cfg.Environment),
			PrivateEndpoint:  true,
			EncryptedSecrets: true,
			LoggingEnabled:   true,
		}, result)

		policies.CheckSPIREOIDC(policies.SPIREOIDCPolicyInput{
			Environment:    cfg.Environment,
			ResourceName:   fmt.Sprintf("forge-%s-spire-oidc-gcp", cfg.Environment),
			OIDCIssuerSet:  true,
			TrustDomainSet: cfg.AWSSPIRETrustDomain != "",
		}, result)
	}

	for _, v := range result.Violations {
		if v.Severity == policies.Advisory {
			fmt.Fprintf(os.Stderr, "POLICY WARNING [%s]: %s\n", v.Policy, v.Message)
		}
	}
	if err := result.Error(); err != nil {
		for _, v := range result.Violations {
			if v.Severity == policies.Mandatory {
				fmt.Fprintf(os.Stderr, "POLICY VIOLATION [%s]: %s\n", v.Policy, v.Message)
			}
		}
		return err
	}
	return nil
}

func gcpFoundationPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig) (*gcp.Network, error) {
	network, err := gcp.NewNetwork(ctx, "forge-network", &gcp.NetworkArgs{
		Environment: cfg.Environment,
		Region:      cfg.GCPRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	return network, nil
}

func awsFoundationPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig) (*awscomp.VPC, error) {
	awsVPC, err := awscomp.NewVPC(ctx, "forge-aws-vpc", &awscomp.VPCArgs{
		Environment: cfg.Environment,
		Region:      cfg.AWSRegion,
		MultiAZNAT:  cfg.EnableMultiAZNAT,
	})
	if err != nil {
		return nil, fmt.Errorf("aws vpc: %w", err)
	}
	return awsVPC, nil
}

func optionalGKEDomainPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig, network *gcp.Network) error {
	if !cfg.EnableGKE {
		return nil
	}
	cluster, err := gcp.NewGKECluster(ctx, "forge-gke", &gcp.GKEClusterArgs{
		Environment: cfg.Environment,
		NetworkID:   network.ID,
		SubnetID:    network.SubnetID,
		NodeCount:   cfg.GKENodeCount,
		MachineType: cfg.GKEMachineType,
	})
	if err != nil {
		return fmt.Errorf("gke: %w", err)
	}

	if _, err = gcp.NewWorkloadIdentity(ctx, "forge-wif", &gcp.WorkloadIdentityArgs{
		Environment:         cfg.Environment,
		SPIRETrustDomain:    cfg.SPIRETrustDomain,
		AWSSPIRETrustDomain: cfg.AWSSPIRETrustDomain,
		GKEClusterName:      cluster.Name,
	}); err != nil {
		return fmt.Errorf("workload identity: %w", err)
	}
	return nil
}

func optionalEKSDomainPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig, awsVPC *awscomp.VPC) error {
	if !cfg.EnableEKS {
		return nil
	}
	_, err := awscomp.NewEKSCluster(ctx, "forge-aws-eks", &awscomp.EKSClusterArgs{
		Environment:  cfg.Environment,
		VpcID:        awsVPC.ID,
		SubnetIDs:    awsVPC.SubnetIDs,
		NodeCount:    cfg.EKSNodeCount,
		InstanceType: cfg.EKSInstanceType,
	})
	if err != nil {
		return fmt.Errorf("aws eks: %w", err)
	}

	if _, err = awscomp.NewSPIREOIDCProvider(ctx, "forge-aws-spire-oidc", &awscomp.SPIREOIDCProviderArgs{
		Environment:         cfg.Environment,
		SPIRETrustDomain:    cfg.AWSSPIRETrustDomain,
		GCPSPIRETrustDomain: cfg.SPIRETrustDomain,
	}); err != nil {
		return fmt.Errorf("aws spire oidc: %w", err)
	}
	return nil
}

func optionalManagedStatePhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig, awsVPC *awscomp.VPC) error {
	if !cfg.EnableManagedState {
		return nil
	}
	if _, err := gcp.NewManagedState(ctx, "forge-gcp-managed-state", &gcp.ManagedStateArgs{
		Environment: cfg.Environment,
		Region:      cfg.GCPRegion,
	}); err != nil {
		return fmt.Errorf("gcp managed-state: %w", err)
	}

	pc := pulumiconfig.New(ctx, "forge")
	dbPassword := pc.RequireSecret("spire-db-password")

	if _, err := awscomp.NewManagedState(ctx, "forge-aws-managed-state", &awscomp.ManagedStateArgs{
		Environment:      cfg.Environment,
		PrivateSubnetIDs: awsVPC.SubnetIDs,
		InternalSGID:     awsVPC.InternalSGID,
		DBPassword:       dbPassword,
	}); err != nil {
		return fmt.Errorf("aws managed-state: %w", err)
	}
	return nil
}

func spireServerPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig, network *gcp.Network, awsVPC *awscomp.VPC) error {
	if _, err := gcp.NewSPIREServer(ctx, "forge-gcp-spire-server", &gcp.SPIREServerArgs{
		Environment:      cfg.Environment,
		Region:           cfg.GCPRegion,
		MgmtSubnetLink:   network.MgmtSubnetLink,
		VPCID:            network.ID,
		SPIREVersion:     cfg.SPIREServerVersion,
		TrustDomain:      cfg.SPIRETrustDomain,
		PeerTrustDomain:  cfg.AWSSPIRETrustDomain,
		ManagedStateMode: cfg.EnableManagedState,
	}); err != nil {
		return fmt.Errorf("gcp spire server: %w", err)
	}

	awsAMI := pulumiconfig.New(ctx, "forge").Get("spire-aws-ami")
	if awsAMI == "" {
		return fmt.Errorf("config forge:spire-aws-ami is required (e.g. Amazon Linux 2023 AMI for %s)", cfg.AWSRegion)
	}

	firstSubnet := awsVPC.SubnetIDs.ApplyT(func(ids []string) string {
		if len(ids) == 0 {
			return ""
		}
		return ids[0]
	}).(pulumi.StringOutput)

	if _, err := awscomp.NewSPIREServer(ctx, "forge-aws-spire-server", &awscomp.SPIREServerArgs{
		Environment:      cfg.Environment,
		Region:           cfg.AWSRegion,
		VPCID:            awsVPC.ID,
		PrivateSubnetID:  firstSubnet,
		InternalSGID:     awsVPC.InternalSGID,
		AMI:              awsAMI,
		SPIREVersion:     cfg.SPIREServerVersion,
		TrustDomain:      cfg.AWSSPIRETrustDomain,
		PeerTrustDomain:  cfg.SPIRETrustDomain,
		ManagedStateMode: cfg.EnableManagedState,
	}); err != nil {
		return fmt.Errorf("aws spire server: %w", err)
	}
	return nil
}

func bowtieControllerPhase(ctx *pulumi.Context, cfg *forgeconfig.ForgeConfig, network *gcp.Network, awsVPC *awscomp.VPC) error {
	if !cfg.EnableBowtie {
		return nil
	}
	if _, err := gcp.NewBowtieController(ctx, "forge-gcp-bowtie", &gcp.BowtieControllerArgs{
		Environment:    cfg.Environment,
		Region:         cfg.GCPRegion,
		MgmtSubnetLink: network.MgmtSubnetLink,
		VPCID:          network.ID,
		Image:          cfg.BowtieGCPImage,
		AdminCIDRs:     cfg.BowtieAdminCIDRs,
	}); err != nil {
		return fmt.Errorf("gcp bowtie: %w", err)
	}

	firstPublicSubnet := awsVPC.PublicSubnetIDs.ApplyT(func(ids []string) string {
		if len(ids) == 0 {
			return ""
		}
		return ids[0]
	}).(pulumi.StringOutput)

	if _, err := awscomp.NewBowtieController(ctx, "forge-aws-bowtie", &awscomp.BowtieControllerArgs{
		Environment:    cfg.Environment,
		Region:         cfg.AWSRegion,
		VPCID:          awsVPC.ID,
		PublicSubnetID: firstPublicSubnet,
		AMI:            cfg.BowtieAWSAMI,
		AdminCIDRs:     cfg.BowtieAdminCIDRs,
	}); err != nil {
		return fmt.Errorf("aws bowtie: %w", err)
	}
	return nil
}

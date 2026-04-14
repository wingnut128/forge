package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/wingnut128/forge/pkg/attestation"
	"github.com/wingnut128/forge/pkg/authz"
	awscomp "github.com/wingnut128/forge/pkg/components/aws"
	"github.com/wingnut128/forge/pkg/components/gcp"
	forgeconfig "github.com/wingnut128/forge/pkg/config"
	"github.com/wingnut128/forge/pkg/orchestration"
	"github.com/wingnut128/forge/pkg/policies"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: forge <preview|up|destroy|serve>")
		os.Exit(1)
	}

	if os.Args[1] == "serve" {
		if err := runServe(); err != nil {
			fmt.Fprintf(os.Stderr, "serve failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()

	stackName := os.Getenv("FORGE_STACK")
	if stackName == "" {
		stackName = "dev"
	}

	s, err := auto.UpsertStackInlineSource(ctx, stackName, "forge", deployFunc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create/select stack: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("operating on stack %q\n", stackName)

	w := os.Stdout

	switch os.Args[1] {
	case "preview":
		_, err = s.Preview(ctx, optpreview.ProgressStreams(w))
	case "up":
		_, err = s.Up(ctx, optup.ProgressStreams(w))
	case "destroy":
		_, err = s.Destroy(ctx, optdestroy.ProgressStreams(w))
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "command failed: %v\n", err)
		os.Exit(1)
	}
}

// deployFunc is the inline Pulumi program invoked by the Automation API.
func deployFunc(ctx *pulumi.Context) error {
	cfg, err := forgeconfig.Load(ctx)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

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

	// --- GCP foundation ---
	network, err := gcp.NewNetwork(ctx, "forge-network", &gcp.NetworkArgs{
		Environment: cfg.Environment,
		Region:      cfg.GCPRegion,
	})
	if err != nil {
		return fmt.Errorf("network: %w", err)
	}

	// --- AWS foundation ---
	awsVPC, err := awscomp.NewVPC(ctx, "forge-aws-vpc", &awscomp.VPCArgs{
		Environment: cfg.Environment,
		Region:      cfg.AWSRegion,
		MultiAZNAT:  cfg.EnableMultiAZNAT,
	})
	if err != nil {
		return fmt.Errorf("aws vpc: %w", err)
	}

	// --- Optional GKE/EKS tracks ---
	if cfg.EnableGKE {
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
			Environment:      cfg.Environment,
			SPIRETrustDomain: cfg.SPIRETrustDomain,
			AWSSTrustDomain:  cfg.AWSSPIRETrustDomain,
			GKEClusterName:   cluster.Name,
		}); err != nil {
			return fmt.Errorf("workload identity: %w", err)
		}
	}

	if cfg.EnableEKS {
		eksCluster, err := awscomp.NewEKSCluster(ctx, "forge-aws-eks", &awscomp.EKSClusterArgs{
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
			EKSClusterName:      eksCluster.Name,
		}); err != nil {
			return fmt.Errorf("aws spire oidc: %w", err)
		}
	}

	// --- Managed-state track (optional) ---
	if cfg.EnableManagedState {
		if _, err := gcp.NewManagedState(ctx, "forge-gcp-managed-state", &gcp.ManagedStateArgs{
			Environment: cfg.Environment,
			Region:      cfg.GCPRegion,
		}); err != nil {
			return fmt.Errorf("gcp managed-state: %w", err)
		}

		pc := pulumiconfig.New(ctx, "forge")
		dbPassword := pc.RequireSecret("spire-db-password")

		// First private subnet is AZ a; pass both via awsVPC.SubnetIDs.
		if _, err := awscomp.NewManagedState(ctx, "forge-aws-managed-state", &awscomp.ManagedStateArgs{
			Environment:      cfg.Environment,
			VPCID:            awsVPC.ID,
			PrivateSubnetIDs: awsVPC.SubnetIDs,
			InternalSGID:     awsVPC.InternalSGID,
			DBPassword:       dbPassword,
		}); err != nil {
			return fmt.Errorf("aws managed-state: %w", err)
		}
	}

	// --- SPIRE servers on cheap VMs ---
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

	// AWS SPIRE server: needs an AMI. Resolve one if bowtie-aws-ami was provided,
	// otherwise expect operator to set `forge:spire-aws-ami` (Amazon Linux 2023).
	awsAMI := pulumiconfig.New(ctx, "forge").Get("spire-aws-ami")
	if awsAMI == "" {
		return fmt.Errorf("config forge:spire-aws-ami is required (e.g. Amazon Linux 2023 AMI for %s)", cfg.AWSRegion)
	}

	// Read first subnet index — SPIRE server sits in AZ a.
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

	// --- Bowtie controllers ---
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

func runServe() error {
	localTD := os.Getenv("FORGE_LOCAL_TRUST_DOMAIN")
	remoteTD := os.Getenv("FORGE_REMOTE_TRUST_DOMAIN")
	bundleURL := os.Getenv("FORGE_BUNDLE_ENDPOINT_URL")
	listenAddr := os.Getenv("FORGE_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	if localTD == "" || remoteTD == "" || bundleURL == "" {
		return fmt.Errorf("FORGE_LOCAL_TRUST_DOMAIN, FORGE_REMOTE_TRUST_DOMAIN, and FORGE_BUNDLE_ENDPOINT_URL are required")
	}

	pair, err := attestation.NewFederationPair(
		attestation.TrustDomain{Name: localTD, Cloud: "gcp"},
		attestation.TrustDomain{Name: remoteTD, Cloud: "aws"},
	)
	if err != nil {
		return fmt.Errorf("federation pair: %w", err)
	}

	refresher, err := attestation.NewBundleRefresher(remoteTD, bundleURL, 0)
	if err != nil {
		return fmt.Errorf("bundle refresher: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := refresher.Start(ctx); err != nil {
		return fmt.Errorf("starting bundle refresher: %w", err)
	}

	var authorizer authz.Authorizer
	policyDir := os.Getenv("FORGE_POLICY_DIR")
	if policyDir != "" {
		a, err := authz.NewCedarAuthorizer(policyDir)
		if err != nil {
			return fmt.Errorf("loading policies from %s: %w", policyDir, err)
		}
		authorizer = a
		fmt.Printf("loaded Cedar policies from %s\n", policyDir)
	}

	srv := orchestration.NewServer(pair, refresher, listenAddr, authorizer)
	fmt.Printf("forge serve listening on %s\n", listenAddr)
	return srv.Start(ctx)
}

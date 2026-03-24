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

	"github.com/wingnut128/forge/pkg/attestation"
	"github.com/wingnut128/forge/pkg/authz"
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

	// Handle serve separately — no Pulumi context needed.
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

	// Wire up streaming output
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

	// --- Policy checks ---
	result := &policies.Result{}

	policies.CheckNetwork(policies.NetworkPolicyInput{
		Environment:         cfg.Environment,
		ResourceName:        fmt.Sprintf("forge-%s-vpc", cfg.Environment),
		CustomSubnetMode:    true, // hardcoded in NewNetwork
		PrivateGoogleAccess: true, // hardcoded in NewNetwork
	}, result)

	policies.CheckGKE(policies.GKEPolicyInput{
		Environment:       cfg.Environment,
		ResourceName:      fmt.Sprintf("forge-%s-gke", cfg.Environment),
		PrivateCluster:    true, // hardcoded in NewGKECluster
		WorkloadIdentity:  true, // hardcoded in NewGKECluster
		BinaryAuthEnabled: true, // hardcoded in NewGKECluster
		NetworkPolicy:     true, // hardcoded in NewGKECluster
		SecureBoot:        true, // hardcoded in NewGKECluster
		IntegrityMonitor:  true, // hardcoded in NewGKECluster
		AutoRepair:        true, // hardcoded in NewGKECluster
		AutoUpgrade:       true, // hardcoded in NewGKECluster
	}, result)

	policies.CheckWorkloadIdentity(policies.WorkloadIdentityPolicyInput{
		Environment:        cfg.Environment,
		ResourceName:       fmt.Sprintf("forge-%s-spiffe-pool", cfg.Environment),
		AttributeCondition: fmt.Sprintf("assertion.sub.startsWith('spiffe://%s/')", cfg.AWSSPIRETrustDomain),
		HasAudiences:       cfg.SPIRETrustDomain != "",
	}, result)

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

	// --- Provision infrastructure ---

	// GCP network foundation
	network, err := gcp.NewNetwork(ctx, "forge-network", &gcp.NetworkArgs{
		Environment: cfg.Environment,
	})
	if err != nil {
		return fmt.Errorf("network: %w", err)
	}

	// GKE cluster for SPIRE server + workloads
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

	// Workload Identity Federation for cross-cloud SPIFFE attestation
	_, err = gcp.NewWorkloadIdentity(ctx, "forge-wif", &gcp.WorkloadIdentityArgs{
		Environment:      cfg.Environment,
		SPIRETrustDomain: cfg.SPIRETrustDomain,
		AWSSTrustDomain:  cfg.AWSSPIRETrustDomain,
		GKEClusterName:   cluster.Name,
	})
	if err != nil {
		return fmt.Errorf("workload identity: %w", err)
	}

	return nil
}

// runServe starts the attestation HTTP server.
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

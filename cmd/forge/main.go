package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/wingnut128/forge/pkg/components/gcp"
	forgeconfig "github.com/wingnut128/forge/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: forge <preview|up|destroy>")
		os.Exit(1)
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
		Environment:       cfg.Environment,
		SPIRETrustDomain:  cfg.SPIRETrustDomain,
		AWSSTrustDomain:   cfg.AWSSPIRETrustDomain,
		GKEClusterName:    cluster.Name,
	})
	if err != nil {
		return fmt.Errorf("workload identity: %w", err)
	}

	return nil
}

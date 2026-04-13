package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optrefresh"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/wingnut128/forge/pkg/components/gcp"
)

func runTest(bgCtx context.Context, stackName string) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: forge test <preview|up|destroy>")
	}

	gcpProject := os.Getenv("GCP_PROJECT")
	if gcpProject == "" {
		return fmt.Errorf("GCP_PROJECT environment variable is required")
	}

	s, err := auto.UpsertStackInlineSource(bgCtx, stackName, "forge-test", testDeployFunc)
	if err != nil {
		return fmt.Errorf("failed to create/select stack: %w", err)
	}

	if err := s.SetConfig(bgCtx, "gcp:project", auto.ConfigValue{Value: gcpProject}); err != nil {
		return fmt.Errorf("failed to set gcp:project config: %w", err)
	}

	fmt.Printf("operating on test stack %q (project: %s)\n", stackName, gcpProject)

	switch os.Args[2] {
	case "preview":
		_, err = s.Preview(bgCtx, optpreview.ProgressStreams(os.Stdout))
	case "up":
		_, err = s.Up(bgCtx, optup.ProgressStreams(os.Stdout))
	case "refresh":
		_, err = s.Refresh(bgCtx, optrefresh.ProgressStreams(os.Stdout))
	case "destroy":
		_, err = s.Destroy(bgCtx, optdestroy.ProgressStreams(os.Stdout))
	default:
		return fmt.Errorf("unknown test command: %s", os.Args[2])
	}
	return err
}

func testDeployFunc(ctx *pulumi.Context) error {
	cfg := config.New(ctx, "forge-test")

	environment := cfg.Get("environment")
	if environment == "" {
		environment = "test"
	}

	gcpCfg := config.New(ctx, "gcp")
	gcpProject := gcpCfg.Require("project")

	containerImage := cfg.Get("container-image")
	if containerImage == "" {
		containerImage = fmt.Sprintf("us-central1-docker.pkg.dev/%s/forge-%s/netcidr:latest", gcpProject, environment)
	}

	network, err := gcp.NewNetwork(ctx, "test-network", &gcp.NetworkArgs{
		Environment: environment,
	})
	if err != nil {
		return fmt.Errorf("network: %w", err)
	}

	_, err = gcp.NewArtifactRegistry(ctx, "test-registry", &gcp.ArtifactRegistryArgs{
		Environment: environment,
	})
	if err != nil {
		return fmt.Errorf("artifact registry: %w", err)
	}

	service, err := gcp.NewCloudRunService(ctx, "test-cloudrun", &gcp.CloudRunServiceArgs{
		Environment: environment,
		Image:       pulumi.String(containerImage),
		Args:        []string{"serve", "--address", "0.0.0.0", "--port", "8080"},
	})
	if err != nil {
		return fmt.Errorf("cloud run: %w", err)
	}

	arRepo := fmt.Sprintf("forge-%s", environment)
	serviceName := fmt.Sprintf("forge-%s-service", environment)

	_, err = gcp.NewCloudBuildTrigger(ctx, "test-cloudbuild", &gcp.CloudBuildTriggerArgs{
		Environment: environment,
		Region:      "us-central1",
		RepoOwner:   "wingnut128",
		RepoName:    "netcidr",
		ARRepo:      arRepo,
		ServiceName: serviceName,
	})
	if err != nil {
		return fmt.Errorf("cloudbuild trigger: %w", err)
	}

	ctx.Export("vpcId", network.ID)
	ctx.Export("serviceUrl", service.URL)

	return nil
}

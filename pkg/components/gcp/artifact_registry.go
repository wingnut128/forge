package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/artifactregistry"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ArtifactRegistryArgs struct {
	Environment string
}

type ArtifactRegistry struct {
	pulumi.ResourceState

	ID             pulumi.IDOutput
	RepositoryName pulumi.StringOutput
}

func NewArtifactRegistry(ctx *pulumi.Context, name string, args *ArtifactRegistryArgs, opts ...pulumi.ResourceOption) (*ArtifactRegistry, error) {
	component := &ArtifactRegistry{}
	err := ctx.RegisterComponentResource("forge:gcp:ArtifactRegistry", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	repo, err := artifactregistry.NewRepository(ctx, namePrefix+"-repo", &artifactregistry.RepositoryArgs{
		Format:       pulumi.String("DOCKER"),
		Location:     pulumi.String("us-central1"),
		RepositoryId: pulumi.String(namePrefix),
		Description:  pulumi.Sprintf("Forge %s container images", args.Environment),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.ID = repo.ID()
	component.RepositoryName = repo.Name

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"repositoryId":   repo.ID(),
		"repositoryName": repo.Name,
	}); err != nil {
		return nil, err
	}

	return component, nil
}

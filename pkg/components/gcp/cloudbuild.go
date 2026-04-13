package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudbuild"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type CloudBuildTriggerArgs struct {
	Environment string
	Region      string
	RepoOwner   string
	RepoName    string
	ARRepo      string
	ServiceName string
	TagPattern  string
}

type CloudBuildTrigger struct {
	pulumi.ResourceState

	TriggerID pulumi.StringOutput
}

func NewCloudBuildTrigger(ctx *pulumi.Context, name string, args *CloudBuildTriggerArgs, opts ...pulumi.ResourceOption) (*CloudBuildTrigger, error) {
	component := &CloudBuildTrigger{}
	err := ctx.RegisterComponentResource("forge:gcp:CloudBuildTrigger", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	tagPattern := args.TagPattern
	if tagPattern == "" {
		tagPattern = "^v.*"
	}

	trigger, err := cloudbuild.NewTrigger(ctx, namePrefix+"-"+args.RepoName+"-trigger", &cloudbuild.TriggerArgs{
		Location: pulumi.String(args.Region),
		Name:     pulumi.String(namePrefix + "-" + args.RepoName),
		Filename: pulumi.String("cloudbuild.yaml"),
		Substitutions: pulumi.StringMap{
			"_REGION":       pulumi.String(args.Region),
			"_AR_REPO":      pulumi.String(args.ARRepo),
			"_SERVICE_NAME": pulumi.String(args.ServiceName),
		},
		Github: &cloudbuild.TriggerGithubArgs{
			Owner: pulumi.String(args.RepoOwner),
			Name:  pulumi.String(args.RepoName),
			Push: &cloudbuild.TriggerGithubPushArgs{
				Tag: pulumi.String(tagPattern),
			},
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.TriggerID = trigger.TriggerId

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"triggerId": trigger.TriggerId,
	}); err != nil {
		return nil, err
	}

	return component, nil
}

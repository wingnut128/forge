package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type CloudRunServiceArgs struct {
	Environment        string
	Region             string
	Image              pulumi.StringInput
	Port               int
	Command            []string
	Args               []string
	DeletionProtection bool
}

type CloudRunService struct {
	pulumi.ResourceState

	ID  pulumi.IDOutput
	URL pulumi.StringOutput
}

func NewCloudRunService(ctx *pulumi.Context, name string, args *CloudRunServiceArgs, opts ...pulumi.ResourceOption) (*CloudRunService, error) {
	component := &CloudRunService{}
	err := ctx.RegisterComponentResource("forge:gcp:CloudRunService", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	port := args.Port
	if port == 0 {
		port = 8080
	}

	service, err := cloudrunv2.NewService(ctx, namePrefix+"-service", &cloudrunv2.ServiceArgs{
		Location:           pulumi.String(args.Region),
		DeletionProtection: pulumi.Bool(args.DeletionProtection),
		Template: &cloudrunv2.ServiceTemplateArgs{
			Containers: cloudrunv2.ServiceTemplateContainerArray{
				containerArgs(args, port),
			},
			Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
				MinInstanceCount: pulumi.Int(0),
				MaxInstanceCount: pulumi.Int(2),
			},
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	_, err = cloudrunv2.NewServiceIamMember(ctx, namePrefix+"-public", &cloudrunv2.ServiceIamMemberArgs{
		Name:     service.Name,
		Location: pulumi.String(args.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.String("allUsers"),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.ID = service.ID()
	component.URL = service.Uri

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"serviceId":  service.ID(),
		"serviceUrl": service.Uri,
	}); err != nil {
		return nil, err
	}

	return component, nil
}

func containerArgs(args *CloudRunServiceArgs, port int) *cloudrunv2.ServiceTemplateContainerArgs {
	c := &cloudrunv2.ServiceTemplateContainerArgs{
		Image: args.Image,
		Ports: &cloudrunv2.ServiceTemplateContainerPortsArgs{
			ContainerPort: pulumi.Int(port),
		},
		Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
			Limits: pulumi.StringMap{
				"cpu":    pulumi.String("1"),
				"memory": pulumi.String("512Mi"),
			},
		},
	}
	if len(args.Command) > 0 {
		cmd := make(pulumi.StringArray, len(args.Command))
		for i, v := range args.Command {
			cmd[i] = pulumi.String(v)
		}
		c.Commands = cmd
	}
	if len(args.Args) > 0 {
		a := make(pulumi.StringArray, len(args.Args))
		for i, v := range args.Args {
			a[i] = pulumi.String(v)
		}
		c.Args = a
	}
	return c
}

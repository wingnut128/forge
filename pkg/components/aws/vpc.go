package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// VPCArgs configures the AWS VPC component.
type VPCArgs struct {
	Environment string
}

// VPC is a Pulumi component resource that provisions an AWS VPC with
// private subnets across two availability zones.
type VPC struct {
	pulumi.ResourceState

	ID        pulumi.IDOutput
	SubnetIDs pulumi.StringArrayOutput
}

// NewVPC creates the VPC, subnets, and security group.
func NewVPC(ctx *pulumi.Context, name string, args *VPCArgs, opts ...pulumi.ResourceOption) (*VPC, error) {
	component := &VPC{}
	err := ctx.RegisterComponentResource("forge:aws:VPC", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	// VPC
	vpc, err := ec2.NewVpc(ctx, namePrefix+"-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String("10.1.0.0/16"),
		EnableDnsSupport:   pulumi.Bool(true),
		EnableDnsHostnames: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-vpc"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Private subnets in two AZs
	subnetA, err := ec2.NewSubnet(ctx, namePrefix+"-subnet-a", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.0.0/20"),
		AvailabilityZone:    pulumi.String("us-east-1a"),
		MapPublicIpOnLaunch: pulumi.Bool(false),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-subnet-a"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	subnetB, err := ec2.NewSubnet(ctx, namePrefix+"-subnet-b", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.16.0/20"),
		AvailabilityZone:    pulumi.String("us-east-1b"),
		MapPublicIpOnLaunch: pulumi.Bool(false),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-subnet-b"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Security group allowing internal traffic
	_, err = ec2.NewSecurityGroup(ctx, namePrefix+"-sg-internal", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.Sprintf("Forge %s internal traffic", args.Environment),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String("10.1.0.0/16")},
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-sg-internal"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.ID = vpc.ID()
	component.SubnetIDs = pulumi.StringArray{
		subnetA.ID().ToStringOutput(),
		subnetB.ID().ToStringOutput(),
	}.ToStringArrayOutput()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId":     vpc.ID(),
		"subnetIds": component.SubnetIDs,
	}); err != nil {
		return nil, err
	}

	return component, nil
}

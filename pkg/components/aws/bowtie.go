package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// BowtieControllerArgs configures the AWS Bowtie controller EC2 instance.
type BowtieControllerArgs struct {
	Environment     string
	Region          string
	VPCID           pulumi.IDOutput
	PublicSubnetID  pulumi.StringOutput
	AMI             string
	InstanceType    string // default: t3.small
	AdminCIDRs      []string
}

// BowtieController provisions a single Bowtie controller EC2 instance with an
// Elastic IP in a public subnet.
type BowtieController struct {
	pulumi.ResourceState

	InstanceID pulumi.IDOutput
	PublicIP   pulumi.StringOutput
	PrivateIP  pulumi.StringOutput
}

// NewBowtieController provisions the EC2 instance, EIP, and admin security group.
func NewBowtieController(ctx *pulumi.Context, name string, args *BowtieControllerArgs, opts ...pulumi.ResourceOption) (*BowtieController, error) {
	if args.AMI == "" {
		return nil, fmt.Errorf("bowtie-aws-ami config is required when Bowtie is enabled")
	}
	component := &BowtieController{}
	if err := ctx.RegisterComponentResource("forge:aws:BowtieController", name, component, opts...); err != nil {
		return nil, err
	}
	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	instanceType := args.InstanceType
	if instanceType == "" {
		instanceType = "t3.small"
	}

	adminCIDRs := args.AdminCIDRs
	if len(adminCIDRs) == 0 {
		adminCIDRs = []string{"127.0.0.1/32"}
	}
	adminPulumi := pulumi.StringArray{}
	for _, c := range adminCIDRs {
		adminPulumi = append(adminPulumi, pulumi.String(c))
	}

	sg, err := ec2.NewSecurityGroup(ctx, namePrefix+"-sg-bowtie", &ec2.SecurityGroupArgs{
		VpcId:       args.VPCID.ToStringOutput(),
		Description: pulumi.String("Forge Bowtie controller"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("tcp"),
				FromPort:   pulumi.Int(22),
				ToPort:     pulumi.Int(22),
				CidrBlocks: adminPulumi,
			},
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("tcp"),
				FromPort:   pulumi.Int(443),
				ToPort:     pulumi.Int(443),
				CidrBlocks: adminPulumi,
			},
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("udp"),
				FromPort:   pulumi.Int(51820),
				ToPort:     pulumi.Int(51820),
				CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
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
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-sg-bowtie")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	eip, err := ec2.NewEip(ctx, namePrefix+"-eip-bowtie", &ec2.EipArgs{
		Domain: pulumi.String("vpc"),
		Tags:   pulumi.StringMap{"Name": pulumi.String(namePrefix + "-eip-bowtie")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	instance, err := ec2.NewInstance(ctx, namePrefix+"-bowtie", &ec2.InstanceArgs{
		Ami:                      pulumi.String(args.AMI),
		InstanceType:             pulumi.String(instanceType),
		SubnetId:                 args.PublicSubnetID,
		VpcSecurityGroupIds:      pulumi.StringArray{sg.ID().ToStringOutput()},
		AssociatePublicIpAddress: pulumi.Bool(false),
		RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
			VolumeSize: pulumi.Int(20),
			VolumeType: pulumi.String("gp3"),
		},
		Tags: pulumi.StringMap{
			"Name":             pulumi.String(namePrefix + "-bowtie"),
			"forge:component":  pulumi.String("bowtie-controller"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	_, err = ec2.NewEipAssociation(ctx, namePrefix+"-eip-bowtie-assoc", &ec2.EipAssociationArgs{
		InstanceId:   instance.ID(),
		AllocationId: eip.ID(),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.InstanceID = instance.ID()
	component.PublicIP = eip.PublicIp
	component.PrivateIP = instance.PrivateIp

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"instanceId": instance.ID(),
		"publicIp":   eip.PublicIp,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

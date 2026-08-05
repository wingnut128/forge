package aws

import (
	"encoding/base64"
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/autoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// fck-nat (https://fck-nat.dev) is a NAT instance AMI that replaces a managed
// NAT Gateway at roughly a fifth of the cost. These are the published defaults;
// override them via VPCArgs if the vendor rotates the account or naming.
const (
	DefaultFckNatAMIOwner       = "568608671756"
	DefaultFckNatAMINamePattern = "fck-nat-al2023-*"
	DefaultFckNatInstanceType   = "t4g.nano"
)

// fckNatArgs configures one NAT instance fleet.
type fckNatArgs struct {
	namePrefix     string
	suffix         string // AZ discriminator, e.g. "a"
	vpcID          pulumi.IDOutput
	vpcCIDR        string
	publicSubnetID pulumi.IDOutput
	instanceType   string
	amiOwner       string
	amiNamePattern string
}

// fckNat is a NAT instance fleet fronted by a persistent ENI.
//
// Routing targets the ENI rather than the instance, so when the ASG replaces a
// dead instance the new one re-attaches the same ENI and routing is unchanged.
// No route-table rewrite (and so no Lambda) is needed on replacement.
type fckNat struct {
	// RoutingENIID is the stable 0.0.0.0/0 target for private route tables.
	RoutingENIID pulumi.IDOutput
}

func newFckNat(ctx *pulumi.Context, args fckNatArgs, opts ...pulumi.ResourceOption) (*fckNat, error) {
	name := fmt.Sprintf("%s-fcknat-%s", args.namePrefix, args.suffix)

	instanceType := args.instanceType
	if instanceType == "" {
		instanceType = DefaultFckNatInstanceType
	}
	amiOwner := args.amiOwner
	if amiOwner == "" {
		amiOwner = DefaultFckNatAMIOwner
	}
	amiNamePattern := args.amiNamePattern
	if amiNamePattern == "" {
		amiNamePattern = DefaultFckNatAMINamePattern
	}

	// Only VPC-internal traffic may use the NAT. A 0.0.0.0/0 ingress rule here
	// would turn the instance into an open relay.
	sg, err := ec2.NewSecurityGroup(ctx, name+"-sg", &ec2.SecurityGroupArgs{
		VpcId:       args.vpcID,
		Description: pulumi.String("fck-nat instance: VPC-internal egress only"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String(args.vpcCIDR)},
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
		Tags: pulumi.StringMap{"Name": pulumi.String(name + "-sg")},
	}, opts...)
	if err != nil {
		return nil, err
	}

	// SourceDestCheck must be false or the kernel silently drops every
	// forwarded packet — no error, no log, just timeouts.
	eni, err := ec2.NewNetworkInterface(ctx, name+"-eni", &ec2.NetworkInterfaceArgs{
		SubnetId:        args.publicSubnetID,
		SecurityGroups:  pulumi.StringArray{sg.ID().ToStringOutput()},
		SourceDestCheck: pulumi.Bool(false),
		Tags:            pulumi.StringMap{"Name": pulumi.String(name + "-eni")},
	}, opts...)
	if err != nil {
		return nil, err
	}

	// A stable egress IP, so the GCP side can allowlist a single address.
	eip, err := ec2.NewEip(ctx, name+"-eip", &ec2.EipArgs{
		Domain: pulumi.String("vpc"),
		Tags:   pulumi.StringMap{"Name": pulumi.String(name + "-eip")},
	}, opts...)
	if err != nil {
		return nil, err
	}
	if _, err := ec2.NewEipAssociation(ctx, name+"-eip-assoc", &ec2.EipAssociationArgs{
		AllocationId:       eip.ID(),
		NetworkInterfaceId: eni.ID(),
	}, opts...); err != nil {
		return nil, err
	}

	// The instance attaches the ENI itself at boot, so it needs EC2 write
	// permissions. SSM is included because a NAT failure is otherwise
	// undiagnosable — there is no other way onto the box.
	role, err := iam.NewRole(ctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "ec2.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}`),
		Tags: pulumi.StringMap{"Name": pulumi.String(name + "-role")},
	}, opts...)
	if err != nil {
		return nil, err
	}
	if _, err := iam.NewRolePolicy(ctx, name+"-policy", &iam.RolePolicyArgs{
		Role: role.ID(),
		Policy: pulumi.String(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "ec2:AttachNetworkInterface",
      "ec2:DetachNetworkInterface",
      "ec2:ModifyNetworkInterfaceAttribute",
      "ec2:DescribeNetworkInterfaces",
      "ec2:AssociateAddress",
      "ec2:DisassociateAddress"
    ],
    "Resource": "*"
  }]
}`),
	}, opts...); err != nil {
		return nil, err
	}
	if _, err := iam.NewRolePolicyAttachment(ctx, name+"-ssm", &iam.RolePolicyAttachmentArgs{
		Role:      role.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
	}, opts...); err != nil {
		return nil, err
	}
	profile, err := iam.NewInstanceProfile(ctx, name+"-profile", &iam.InstanceProfileArgs{
		Role: role.Name,
	}, opts...)
	if err != nil {
		return nil, err
	}

	ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
		MostRecent: pulumi.BoolRef(true),
		Owners:     []string{amiOwner},
		Filters: []ec2.GetAmiFilter{
			{Name: "name", Values: []string{amiNamePattern}},
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("fck-nat ami lookup (owner %s, name %s): %w", amiOwner, amiNamePattern, err)
	}

	// fck-nat reads eni_id from its config file and attaches that ENI at boot.
	userData := eni.ID().ApplyT(func(id pulumi.ID) string {
		script := fmt.Sprintf("#!/bin/bash\necho \"eni_id=%s\" >> /etc/fck-nat.conf\nservice fck-nat restart\n", id)
		return base64.StdEncoding.EncodeToString([]byte(script))
	}).(pulumi.StringOutput)

	// The primary interface needs its own public IP: attaching the ENI is an
	// EC2 API call, which the instance cannot make before it has egress.
	lt, err := ec2.NewLaunchTemplate(ctx, name+"-lt", &ec2.LaunchTemplateArgs{
		ImageId:      pulumi.String(ami.Id),
		InstanceType: pulumi.String(instanceType),
		IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileArgs{
			Arn: profile.Arn,
		},
		NetworkInterfaces: ec2.LaunchTemplateNetworkInterfaceArray{
			&ec2.LaunchTemplateNetworkInterfaceArgs{
				AssociatePublicIpAddress: pulumi.String("true"),
				DeleteOnTermination:      pulumi.String("true"),
				SecurityGroups:           pulumi.StringArray{sg.ID().ToStringOutput()},
			},
		},
		MetadataOptions: &ec2.LaunchTemplateMetadataOptionsArgs{
			HttpTokens:   pulumi.String("required"),
			HttpEndpoint: pulumi.String("enabled"),
		},
		UserData: userData,
		Tags:     pulumi.StringMap{"Name": pulumi.String(name + "-lt")},
	}, opts...)
	if err != nil {
		return nil, err
	}

	// Capacity is pinned at exactly one: this is a self-healing single
	// instance, not a scaled fleet. The ASG's only job is replacement.
	if _, err := autoscaling.NewGroup(ctx, name+"-asg", &autoscaling.GroupArgs{
		MinSize:            pulumi.Int(1),
		MaxSize:            pulumi.Int(1),
		DesiredCapacity:    pulumi.Int(1),
		VpcZoneIdentifiers: pulumi.StringArray{args.publicSubnetID.ToStringOutput()},
		HealthCheckType:    pulumi.String("EC2"),
		LaunchTemplate: &autoscaling.GroupLaunchTemplateArgs{
			Id:      lt.ID(),
			Version: pulumi.String("$Latest"),
		},
		Tags: autoscaling.GroupTagArray{
			&autoscaling.GroupTagArgs{
				Key:               pulumi.String("Name"),
				Value:             pulumi.String(name),
				PropagateAtLaunch: pulumi.Bool(true),
			},
			&autoscaling.GroupTagArgs{
				Key:               pulumi.String("forge:component"),
				Value:             pulumi.String("fck-nat"),
				PropagateAtLaunch: pulumi.Bool(true),
			},
		},
	}, opts...); err != nil {
		return nil, err
	}

	return &fckNat{RoutingENIID: eni.ID()}, nil
}

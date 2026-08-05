package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// VPCCIDR is the AWS-side address space, chosen not to overlap GCP.
const VPCCIDR = "10.1.0.0/16"

// SPIREServerPrivateIP is pinned so the GCP peer can address the bundle
// endpoint without discovering it. A dynamic address would be circular: each
// cloud's SPIRE config would need the other's instance to exist first. It sits
// in private subnet A (10.1.0.0/20).
const SPIREServerPrivateIP = "10.1.0.10"

// VPCArgs configures the AWS VPC component.
type VPCArgs struct {
	Environment string
	Region      string
	// MultiAZNAT provisions a NAT instance per AZ. When false (default), a
	// single NAT instance in AZ a serves both private subnets to save cost.
	MultiAZNAT bool
	// NATInstanceType overrides the fck-nat instance size (default t4g.nano).
	NATInstanceType string
	// FckNatAMIOwner, FckNatAMINamePattern, and FckNatAMIArchitecture override
	// AMI discovery if the vendor rotates its publishing account or image
	// naming. The architecture must match NATInstanceType: the default arm64
	// suits t4g, and switching to an x86 instance type requires setting this
	// to "x86_64" as well.
	FckNatAMIOwner        string
	FckNatAMINamePattern  string
	FckNatAMIArchitecture string

	// WireGuard settings for the cross-cloud tunnel, terminated on the AZ-a NAT
	// instance. Empty WGPrivateKey leaves the NAT instances as plain NAT.
	WGPrivateKey    string
	WGPeerPublicKey string
	// WGPeerEndpoint is the GCP gateway's reserved public IP.
	WGPeerEndpoint pulumi.StringOutput
	// WGPeerCIDR is the GCP address space reachable through the tunnel.
	WGPeerCIDR string
}

// VPC is a Pulumi component resource that provisions an AWS VPC with:
//   - two private subnets across AZs a and b (for workloads)
//   - two public subnets across AZs a and b (for NAT + Bowtie controller)
//   - Internet Gateway and self-healing fck-nat instance(s) with routing
type VPC struct {
	pulumi.ResourceState

	ID              pulumi.IDOutput
	SubnetIDs       pulumi.StringArrayOutput
	PublicSubnetIDs pulumi.StringArrayOutput
	InternalSGID    pulumi.IDOutput
	// NATPublicIP is the AZ-a NAT instance's stable address — the endpoint the
	// GCP gateway dials for the tunnel.
	NATPublicIP pulumi.StringOutput
}

// NewVPC creates the VPC, subnets, IGW, NAT instances, route tables, and security group.
func NewVPC(ctx *pulumi.Context, name string, args *VPCArgs, opts ...pulumi.ResourceOption) (*VPC, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	component := &VPC{}
	err := ctx.RegisterComponentResource("forge:aws:VPC", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	vpc, err := ec2.NewVpc(ctx, namePrefix+"-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String(VPCCIDR),
		EnableDnsSupport:   pulumi.Bool(true),
		EnableDnsHostnames: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-vpc"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	privateSubnetA, err := ec2.NewSubnet(ctx, namePrefix+"-subnet-a", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.0.0/20"),
		AvailabilityZone:    pulumi.String(args.Region + "a"),
		MapPublicIpOnLaunch: pulumi.Bool(false),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-private-a"),
			"Tier": pulumi.String("private"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	privateSubnetB, err := ec2.NewSubnet(ctx, namePrefix+"-subnet-b", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.16.0/20"),
		AvailabilityZone:    pulumi.String(args.Region + "b"),
		MapPublicIpOnLaunch: pulumi.Bool(false),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-private-b"),
			"Tier": pulumi.String("private"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	publicSubnetA, err := ec2.NewSubnet(ctx, namePrefix+"-public-a", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.32.0/24"),
		AvailabilityZone:    pulumi.String(args.Region + "a"),
		MapPublicIpOnLaunch: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-public-a"),
			"Tier": pulumi.String("public"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	publicSubnetB, err := ec2.NewSubnet(ctx, namePrefix+"-public-b", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.33.0/24"),
		AvailabilityZone:    pulumi.String(args.Region + "b"),
		MapPublicIpOnLaunch: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(namePrefix + "-public-b"),
			"Tier": pulumi.String("public"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Internet Gateway + public route table
	igw, err := ec2.NewInternetGateway(ctx, namePrefix+"-igw", &ec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags:  pulumi.StringMap{"Name": pulumi.String(namePrefix + "-igw")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	publicRT, err := ec2.NewRouteTable(ctx, namePrefix+"-rt-public", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock: pulumi.String("0.0.0.0/0"),
				GatewayId: igw.ID(),
			},
		},
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-rt-public")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	for i, sub := range []*ec2.Subnet{publicSubnetA, publicSubnetB} {
		_, err = ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("%s-rta-public-%d", namePrefix, i), &ec2.RouteTableAssociationArgs{
			SubnetId:     sub.ID(),
			RouteTableId: publicRT.ID(),
		}, parentOpt)
		if err != nil {
			return nil, err
		}
	}

	// NAT instance fleet(s) — single by default, per-AZ if MultiAZNAT is set.
	natA, err := newFckNat(ctx, fckNatArgs{
		namePrefix:     namePrefix,
		suffix:         "a",
		vpcID:          vpc.ID(),
		vpcCIDR:        VPCCIDR,
		publicSubnetID: publicSubnetA.ID(),
		instanceType:   args.NATInstanceType,
		amiOwner:       args.FckNatAMIOwner,
		amiNamePattern: args.FckNatAMINamePattern,
		amiArch:        args.FckNatAMIArchitecture,

		wgPrivateKey:    args.WGPrivateKey,
		wgPeerPublicKey: args.WGPeerPublicKey,
		wgPeerEndpoint:  args.WGPeerEndpoint,
		wgPeerCIDR:      args.WGPeerCIDR,
	}, pulumi.Parent(component), pulumi.DependsOn([]pulumi.Resource{igw}))
	if err != nil {
		return nil, err
	}

	// Private route table for subnet A -> NAT A
	privateRTA, err := ec2.NewRouteTable(ctx, namePrefix+"-rt-private-a", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock:          pulumi.String("0.0.0.0/0"),
				NetworkInterfaceId: natA.RoutingENIID,
			},
		},
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-rt-private-a")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}
	_, err = ec2.NewRouteTableAssociation(ctx, namePrefix+"-rta-private-a", &ec2.RouteTableAssociationArgs{
		SubnetId:     privateSubnetA.ID(),
		RouteTableId: privateRTA.ID(),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Subnet B: either its own NAT (MultiAZNAT) or reuse NAT A.
	privateNatBENI := natA.RoutingENIID
	if args.MultiAZNAT {
		natB, err := newFckNat(ctx, fckNatArgs{
			namePrefix:     namePrefix,
			suffix:         "b",
			vpcID:          vpc.ID(),
			vpcCIDR:        VPCCIDR,
			publicSubnetID: publicSubnetB.ID(),
			instanceType:   args.NATInstanceType,
			amiOwner:       args.FckNatAMIOwner,
			amiNamePattern: args.FckNatAMINamePattern,
			amiArch:        args.FckNatAMIArchitecture,
		}, pulumi.Parent(component), pulumi.DependsOn([]pulumi.Resource{igw}))
		if err != nil {
			return nil, err
		}
		privateNatBENI = natB.RoutingENIID
	}

	privateRTB, err := ec2.NewRouteTable(ctx, namePrefix+"-rt-private-b", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock:          pulumi.String("0.0.0.0/0"),
				NetworkInterfaceId: privateNatBENI,
			},
		},
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-rt-private-b")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}
	_, err = ec2.NewRouteTableAssociation(ctx, namePrefix+"-rta-private-b", &ec2.RouteTableAssociationArgs{
		SubnetId:     privateSubnetB.ID(),
		RouteTableId: privateRTB.ID(),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	internalSG, err := ec2.NewSecurityGroup(ctx, namePrefix+"-sg-internal", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.Sprintf("Forge %s internal traffic", args.Environment),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String(VPCCIDR)},
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
	component.InternalSGID = internalSG.ID()
	component.NATPublicIP = natA.PublicIP
	component.SubnetIDs = pulumi.StringArray{
		privateSubnetA.ID().ToStringOutput(),
		privateSubnetB.ID().ToStringOutput(),
	}.ToStringArrayOutput()
	component.PublicSubnetIDs = pulumi.StringArray{
		publicSubnetA.ID().ToStringOutput(),
		publicSubnetB.ID().ToStringOutput(),
	}.ToStringArrayOutput()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId":           vpc.ID(),
		"subnetIds":       component.SubnetIDs,
		"publicSubnetIds": component.PublicSubnetIDs,
	}); err != nil {
		return nil, err
	}

	return component, nil
}

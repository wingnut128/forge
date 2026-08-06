package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// GCP-side address space.
//
// VPCCIDR spans both subnets below, so it is what the AWS peer routes through
// the tunnel — the SPIRE server lives on the management subnet, which sits
// outside the primary subnet's /20.
const (
	VPCCIDR           = "10.0.0.0/16"
	PrimarySubnetCIDR = "10.0.0.0/20"
	MgmtSubnetCIDR    = "10.0.16.0/24"

	// IAPRange is Google's fixed source range for IAP TCP forwarding.
	IAPRange = "35.235.240.0/20"

	// SPIREServerPrivateIP is pinned so the AWS peer can address the bundle
	// endpoint without discovering it. A dynamic address would be circular:
	// each cloud's SPIRE config would need the other's instance to exist first.
	SPIREServerPrivateIP = "10.0.16.10"
)

// NetworkArgs configures the VPC network component.
type NetworkArgs struct {
	Environment string
	Region      string
}

// Network is a Pulumi component resource that provisions a VPC with:
//   - a primary subnet (with GKE secondary ranges) for workloads
//   - a management subnet for SPIRE server and Bowtie controller VMs
//   - Cloud Router + Cloud NAT so private instances have egress
type Network struct {
	pulumi.ResourceState

	ID             pulumi.IDOutput
	SelfLink       pulumi.StringOutput
	SubnetID       pulumi.IDOutput
	SubnetSelfLink pulumi.StringOutput
	MgmtSubnetID   pulumi.IDOutput
	MgmtSubnetLink pulumi.StringOutput
	Region         string
}

// NewNetwork creates the VPC, subnets, firewall rules, Cloud Router, and Cloud NAT.
func NewNetwork(ctx *pulumi.Context, name string, args *NetworkArgs, opts ...pulumi.ResourceOption) (*Network, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	component := &Network{Region: args.Region}
	err := ctx.RegisterComponentResource("forge:gcp:Network", name, component, opts...)
	if err != nil {
		return nil, err
	}

	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	vpc, err := compute.NewNetwork(ctx, namePrefix+"-vpc", &compute.NetworkArgs{
		AutoCreateSubnetworks: pulumi.Bool(false),
		Description:           pulumi.Sprintf("Forge %s VPC", args.Environment),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	subnet, err := compute.NewSubnetwork(ctx, namePrefix+"-subnet", &compute.SubnetworkArgs{
		Network:     vpc.ID(),
		IpCidrRange: pulumi.String(PrimarySubnetCIDR),
		Region:      pulumi.String(args.Region),
		SecondaryIpRanges: compute.SubnetworkSecondaryIpRangeArray{
			&compute.SubnetworkSecondaryIpRangeArgs{
				RangeName:   pulumi.String("pods"),
				IpCidrRange: pulumi.String("10.4.0.0/14"),
			},
			&compute.SubnetworkSecondaryIpRangeArgs{
				RangeName:   pulumi.String("services"),
				IpCidrRange: pulumi.String("10.8.0.0/20"),
			},
		},
		PrivateIpGoogleAccess: pulumi.Bool(true),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Management subnet for SPIRE server + Bowtie controller VMs.
	// Kept disjoint from the primary /20 and the pod/service secondary ranges.
	mgmtSubnet, err := compute.NewSubnetwork(ctx, namePrefix+"-mgmt-subnet", &compute.SubnetworkArgs{
		Network:               vpc.ID(),
		IpCidrRange:           pulumi.String(MgmtSubnetCIDR),
		Region:                pulumi.String(args.Region),
		PrivateIpGoogleAccess: pulumi.Bool(true),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	_, err = compute.NewFirewall(ctx, namePrefix+"-allow-internal", &compute.FirewallArgs{
		Network: vpc.ID(),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports:    pulumi.StringArray{pulumi.String("0-65535")},
			},
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("udp"),
				Ports:    pulumi.StringArray{pulumi.String("0-65535")},
			},
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("icmp"),
			},
		},
		SourceRanges: pulumi.StringArray{pulumi.String("10.0.0.0/8")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// IAP TCP forwarding is the only way onto the private VMs — they have no
	// public IP and no SSH key. 35.235.240.0/20 is Google's fixed IAP range;
	// access is gated by IAM (roles/iap.tunnelResourceAccessor), not by network
	// position. Without this rule a failed boot is undiagnosable.
	_, err = compute.NewFirewall(ctx, namePrefix+"-allow-iap-ssh", &compute.FirewallArgs{
		Network: vpc.ID(),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports:    pulumi.StringArray{pulumi.String("22")},
			},
		},
		SourceRanges: pulumi.StringArray{pulumi.String(IAPRange)},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Cloud Router + Cloud NAT so private instances can reach package repos,
	// Bowtie licensing endpoints, SPIRE upstream CAs, etc.
	router, err := compute.NewRouter(ctx, namePrefix+"-router", &compute.RouterArgs{
		Network: vpc.ID(),
		Region:  pulumi.String(args.Region),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	_, err = compute.NewRouterNat(ctx, namePrefix+"-nat", &compute.RouterNatArgs{
		Router:                        router.Name,
		Region:                        pulumi.String(args.Region),
		NatIpAllocateOption:           pulumi.String("AUTO_ONLY"),
		SourceSubnetworkIpRangesToNat: pulumi.String("ALL_SUBNETWORKS_ALL_IP_RANGES"),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.ID = vpc.ID()
	component.SelfLink = vpc.SelfLink
	component.SubnetID = subnet.ID()
	component.SubnetSelfLink = subnet.SelfLink
	component.MgmtSubnetID = mgmtSubnet.ID()
	component.MgmtSubnetLink = mgmtSubnet.SelfLink

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId":        vpc.ID(),
		"subnetId":     subnet.ID(),
		"mgmtSubnetId": mgmtSubnet.ID(),
	}); err != nil {
		return nil, err
	}

	return component, nil
}

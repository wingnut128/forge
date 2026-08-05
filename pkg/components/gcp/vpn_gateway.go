package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/wingnut128/forge/pkg/wireguard"
)

// DefaultVPNType is the smallest GCE type; the gateway only shuffles packets.
const DefaultVPNType = "e2-micro"

// NewVPNAddress reserves the gateway's static external IP.
//
// It is separate from NewVPNGateway because the AWS side must allowlist this
// address while building its own NAT instance, which happens before the GCP
// gateway exists. Reserving the address first breaks that cycle.
func NewVPNAddress(ctx *pulumi.Context, name, region string, opts ...pulumi.ResourceOption) (pulumi.StringOutput, error) {
	addr, err := compute.NewAddress(ctx, name, &compute.AddressArgs{
		Region: pulumi.String(region),
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return addr.Address, nil
}

// VPNGatewayArgs configures the GCP side of the cross-cloud WireGuard tunnel.
type VPNGatewayArgs struct {
	Environment string
	Region      string
	Zone        string

	MgmtSubnetLink pulumi.StringOutput
	VPCID          pulumi.IDOutput
	MachineType    string // default: e2-micro

	// ExternalIP is a pre-reserved static address. It is created outside this
	// component so the AWS side can allowlist it before this VM exists —
	// otherwise the two clouds each need the other's endpoint first.
	ExternalIP pulumi.StringOutput

	PrivateKey    string
	PeerPublicKey string
	// PeerEndpointIP is the AWS NAT instance's public IP.
	PeerEndpointIP pulumi.StringOutput
	// PeerCIDR is the AWS VPC address space reachable through the tunnel.
	PeerCIDR string
}

// VPNGateway is a single GCE instance terminating a WireGuard tunnel to AWS.
//
// It exists so the SPIRE server never needs a public IP: the SPIRE VM stays on
// a private subnet and reaches its AWS peer through a VPC route pointed at this
// instance. Only UDP/51820 from the peer's address is exposed.
type VPNGateway struct {
	pulumi.ResourceState

	InstanceName pulumi.StringOutput
	InternalIP   pulumi.StringOutput
	ExternalIP   pulumi.StringOutput
}

// NewVPNGateway provisions the gateway VM, its firewall rule, and the VPC route
// that sends AWS-bound traffic through it.
func NewVPNGateway(ctx *pulumi.Context, name string, args *VPNGatewayArgs, opts ...pulumi.ResourceOption) (*VPNGateway, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	if args.PrivateKey == "" || args.PeerPublicKey == "" {
		return nil, fmt.Errorf("wireguard keys are required when enable-vpn is true")
	}
	if args.PeerCIDR == "" {
		return nil, fmt.Errorf("PeerCIDR is required")
	}

	component := &VPNGateway{}
	if err := ctx.RegisterComponentResource("forge:gcp:VPNGateway", name, component, opts...); err != nil {
		return nil, err
	}
	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	machineType := args.MachineType
	if machineType == "" {
		machineType = DefaultVPNType
	}
	zone := args.Zone
	if zone == "" {
		zone = args.Region + "-a"
	}

	startup := args.PeerEndpointIP.ApplyT(func(peerIP string) (string, error) {
		body, err := wireguard.RenderScript(wireguard.ScriptArgs{
			PackageManager: wireguard.APT,
			Address:        wireguard.GCPTunnelIP + "/30",
			PrivateKey:     args.PrivateKey,
			PeerPublicKey:  args.PeerPublicKey,
			PeerEndpoint:   fmt.Sprintf("%s:%d", peerIP, wireguard.ListenPort),
			AllowedIPs:     fmt.Sprintf("%s,%s", wireguard.TunnelCIDR, args.PeerCIDR),
		})
		if err != nil {
			return "", err
		}
		return "#!/bin/bash\nset -euo pipefail\n" + body, nil
	}).(pulumi.StringOutput)

	// CanIpForward is the GCE equivalent of clearing the source/dest check —
	// without it the VM silently drops packets it is meant to route.
	instance, err := compute.NewInstance(ctx, namePrefix+"-vpn", &compute.InstanceArgs{
		MachineType:  pulumi.String(machineType),
		Zone:         pulumi.String(zone),
		CanIpForward: pulumi.Bool(true),
		BootDisk: &compute.InstanceBootDiskArgs{
			InitializeParams: &compute.InstanceBootDiskInitializeParamsArgs{
				Image: pulumi.String("debian-cloud/debian-12"),
				Size:  pulumi.Int(10),
			},
		},
		NetworkInterfaces: compute.InstanceNetworkInterfaceArray{
			&compute.InstanceNetworkInterfaceArgs{
				Subnetwork: args.MgmtSubnetLink,
				AccessConfigs: compute.InstanceNetworkInterfaceAccessConfigArray{
					&compute.InstanceNetworkInterfaceAccessConfigArgs{
						NatIp: args.ExternalIP,
					},
				},
			},
		},
		MetadataStartupScript: startup,
		Tags:                  pulumi.StringArray{pulumi.String("forge-vpn")},
		Labels: pulumi.StringMap{
			"component":   pulumi.String("vpn-gateway"),
			"environment": pulumi.String(args.Environment),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Only the peer gateway may reach the tunnel port.
	peerCIDR32 := args.PeerEndpointIP.ApplyT(func(ip string) string {
		return ip + "/32"
	}).(pulumi.StringOutput)

	if _, err := compute.NewFirewall(ctx, namePrefix+"-allow-wg", &compute.FirewallArgs{
		Network:   args.VPCID.ToStringOutput(),
		Direction: pulumi.String("INGRESS"),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("udp"),
				Ports:    pulumi.StringArray{pulumi.Sprintf("%d", wireguard.ListenPort)},
			},
		},
		SourceRanges: pulumi.StringArray{peerCIDR32},
		TargetTags:   pulumi.StringArray{pulumi.String("forge-vpn")},
	}, parentOpt); err != nil {
		return nil, err
	}

	// Send AWS-bound traffic to the gateway instead of the default internet route.
	if _, err := compute.NewRoute(ctx, namePrefix+"-route-aws", &compute.RouteArgs{
		Network:             args.VPCID.ToStringOutput(),
		DestRange:           pulumi.String(args.PeerCIDR),
		NextHopInstance:     instance.SelfLink,
		NextHopInstanceZone: pulumi.String(zone),
		Priority:            pulumi.Int(100),
	}, parentOpt); err != nil {
		return nil, err
	}

	component.InstanceName = instance.Name
	component.InternalIP = instance.NetworkInterfaces.Index(pulumi.Int(0)).NetworkIp().Elem()
	component.ExternalIP = args.ExternalIP

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"instanceName": instance.Name,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

// RenderPeerEndpointForTest exposes the hardcoded AWS SPIRE address so a
// cross-package test can assert the two clouds agree.
func RenderPeerEndpointForTest() (string, error) {
	return awsSPIREServerPrivateIP, nil
}

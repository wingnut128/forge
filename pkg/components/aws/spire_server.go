package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// SPIREServerArgs configures the AWS SPIRE server EC2 instance.
type SPIREServerArgs struct {
	Environment      string
	Region           string
	VPCID            pulumi.IDOutput
	PrivateSubnetID  pulumi.StringOutput
	InternalSGID    pulumi.IDOutput
	AMI              string // Amazon Linux 2023 or similar
	InstanceType     string // default: t3.small
	SPIREVersion     string
	TrustDomain      string
	PeerTrustDomain  string
	ManagedStateMode bool
}

// SPIREServer provisions a single EC2 instance + EBS data volume for spire-server.
type SPIREServer struct {
	pulumi.ResourceState

	InstanceID pulumi.IDOutput
	PrivateIP  pulumi.StringOutput
}

// NewSPIREServer provisions the EC2 instance, EBS volume, attachment, and SG.
func NewSPIREServer(ctx *pulumi.Context, name string, args *SPIREServerArgs, opts ...pulumi.ResourceOption) (*SPIREServer, error) {
	component := &SPIREServer{}
	if err := ctx.RegisterComponentResource("forge:aws:SPIREServer", name, component, opts...); err != nil {
		return nil, err
	}
	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	instanceType := args.InstanceType
	if instanceType == "" {
		instanceType = "t3.small"
	}

	sg, err := ec2.NewSecurityGroup(ctx, namePrefix+"-sg-spire", &ec2.SecurityGroupArgs{
		VpcId:       args.VPCID.ToStringOutput(),
		Description: pulumi.String("Forge SPIRE server traffic"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("tcp"),
				FromPort:   pulumi.Int(8081),
				ToPort:     pulumi.Int(8443),
				CidrBlocks: pulumi.StringArray{pulumi.String("10.0.0.0/8")},
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
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-sg-spire")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	userData := spireAWSUserData(args)

	instance, err := ec2.NewInstance(ctx, namePrefix+"-spire-server", &ec2.InstanceArgs{
		Ami:                      pulumi.String(args.AMI),
		InstanceType:             pulumi.String(instanceType),
		SubnetId:                 args.PrivateSubnetID,
		VpcSecurityGroupIds:      pulumi.StringArray{sg.ID().ToStringOutput(), args.InternalSGID.ToStringOutput()},
		AssociatePublicIpAddress: pulumi.Bool(false),
		UserData:                 pulumi.String(userData),
		RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
			VolumeSize: pulumi.Int(10),
			VolumeType: pulumi.String("gp3"),
		},
		EbsBlockDevices: ec2.InstanceEbsBlockDeviceArray{
			&ec2.InstanceEbsBlockDeviceArgs{
				DeviceName: pulumi.String("/dev/xvdf"),
				VolumeSize: pulumi.Int(20),
				VolumeType: pulumi.String("gp3"),
				Encrypted:  pulumi.Bool(true),
				Tags: pulumi.StringMap{
					"Name":             pulumi.String(namePrefix + "-spire-data"),
					"forge:snapshot":   pulumi.String("spire"),
				},
			},
		},
		Tags: pulumi.StringMap{
			"Name":             pulumi.String(namePrefix + "-spire-server"),
			"forge:component":  pulumi.String("spire-server"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.InstanceID = instance.ID()
	component.PrivateIP = instance.PrivateIp

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"instanceId": instance.ID(),
	}); err != nil {
		return nil, err
	}
	return component, nil
}

func spireAWSUserData(args *SPIREServerArgs) string {
	mode := "disk"
	if args.ManagedStateMode {
		mode = "managed"
	}
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
SPIRE_VERSION=%q
STATE_MODE=%q
TRUST_DOMAIN=%q

# Mount data volume at /var/lib/spire
DEV=/dev/xvdf
mkdir -p /var/lib/spire
if ! blkid "$DEV" >/dev/null 2>&1; then
  mkfs.ext4 -F "$DEV"
fi
if ! mountpoint -q /var/lib/spire; then
  echo "$DEV /var/lib/spire ext4 defaults,nofail 0 2" >> /etc/fstab
  mount /var/lib/spire
fi

if [ ! -x /usr/local/bin/spire-server ]; then
  cd /tmp
  curl -sSL -o spire.tar.gz "https://github.com/spiffe/spire/releases/download/v${SPIRE_VERSION}/spire-${SPIRE_VERSION}-linux-amd64-musl.tar.gz"
  tar -xzf spire.tar.gz
  install -m 0755 spire-${SPIRE_VERSION}/bin/spire-server /usr/local/bin/spire-server
fi

mkdir -p /etc/spire
cat >/etc/spire/server.conf <<CONF
server {
  bind_address = "0.0.0.0"
  bind_port = "8081"
  trust_domain = "${TRUST_DOMAIN}"
  data_dir = "/var/lib/spire/data"
  log_level = "INFO"
}
# state mode: ${STATE_MODE}
CONF

cat >/etc/systemd/system/spire-server.service <<UNIT
[Unit]
Description=SPIRE Server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/spire-server run -config /etc/spire/server.conf
Restart=always

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now spire-server
`, args.SPIREVersion, mode, args.TrustDomain)
}

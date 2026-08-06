package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/wingnut128/forge/pkg/spire"
)

// gcpSPIREServerPrivateIP mirrors gcp.SPIREServerPrivateIP. It is duplicated
// rather than imported to keep the cloud packages independent of each other;
// TestSPIREPeerAddressesAgree in cmd/forge guards against drift.
const gcpSPIREServerPrivateIP = "10.0.16.10"

// SPIREServerArgs configures the AWS SPIRE server EC2 instance.
type SPIREServerArgs struct {
	Environment      string
	Region           string
	VPCID            pulumi.IDOutput
	PrivateSubnetID  pulumi.StringOutput
	InternalSGID     pulumi.IDOutput
	AMI              string // Amazon Linux 2023 or similar
	InstanceType     string // default: t3.small
	SPIREVersion     string
	TrustDomain      string
	PeerTrustDomain  string
	ManagedStateMode bool
	// ForgeRepoRef is the git ref of this repo built for `forge serve`.
	// Defaults to DefaultForgeRepoRef.
	ForgeRepoRef string
}

// SPIREServer provisions a single EC2 instance + EBS data volume for spire-server.
type SPIREServer struct {
	pulumi.ResourceState

	InstanceID pulumi.IDOutput
	PrivateIP  pulumi.StringOutput
}

// NewSPIREServer provisions the EC2 instance, EBS volume, attachment, and SG.
func NewSPIREServer(ctx *pulumi.Context, name string, args *SPIREServerArgs, opts ...pulumi.ResourceOption) (*SPIREServer, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
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

	userData, err := spireAWSUserData(args)
	if err != nil {
		return nil, fmt.Errorf("spire server user data: %w", err)
	}

	// The instance sits in a private subnet with no key pair, so SSM Session
	// Manager is the only way onto it. Without this role a failed boot — a bad
	// download, a dead NAT, a crash-looping spire-server — is undiagnosable.
	role, err := iam.NewRole(ctx, namePrefix+"-spire-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "ec2.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}`),
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-spire-role")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}
	if _, err := iam.NewRolePolicyAttachment(ctx, namePrefix+"-spire-ssm", &iam.RolePolicyAttachmentArgs{
		Role:      role.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
	}, parentOpt); err != nil {
		return nil, err
	}
	profile, err := iam.NewInstanceProfile(ctx, namePrefix+"-spire-profile", &iam.InstanceProfileArgs{
		Role: role.Name,
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	instance, err := ec2.NewInstance(ctx, namePrefix+"-spire-server", &ec2.InstanceArgs{
		Ami:                      pulumi.String(args.AMI),
		InstanceType:             pulumi.String(instanceType),
		SubnetId:                 args.PrivateSubnetID,
		PrivateIp:                pulumi.String(SPIREServerPrivateIP),
		VpcSecurityGroupIds:      pulumi.StringArray{sg.ID().ToStringOutput(), args.InternalSGID.ToStringOutput()},
		AssociatePublicIpAddress: pulumi.Bool(false),
		IamInstanceProfile:       profile.Name,
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
					"Name":           pulumi.String(namePrefix + "-spire-data"),
					"forge:snapshot": pulumi.String("spire"),
				},
			},
		},
		Tags: pulumi.StringMap{
			"Name":            pulumi.String(namePrefix + "-spire-server"),
			"forge:component": pulumi.String("spire-server"),
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

func spireAWSUserData(args *SPIREServerArgs) (string, error) {
	mode := spire.StateModeDisk
	if args.ManagedStateMode {
		mode = spire.StateModeManaged
	}
	serverHCL, err := spire.RenderServerHCL(spire.ServerConfig{
		TrustDomain:     args.TrustDomain,
		PeerTrustDomain: args.PeerTrustDomain,
		// The peer is addressed by its pinned private IP, routed over the
		// WireGuard tunnel — a SPIFFE trust domain is an identifier, not an
		// address, and nothing resolves it.
		PeerBundleEndpointURL: fmt.Sprintf("https://%s:8443", gcpSPIREServerPrivateIP),
		StateMode:             mode,
		ManagedDBConnString:   "postgres://spire@127.0.0.1:5432/spire", // Phase 2: real managed DSN
	})
	if err != nil {
		return "", err
	}

	script := spire.RenderServerStartupScript(args.SPIREVersion, serverHCL, "/dev/xvdf")

	// The validator runs alongside the AWS SPIRE server. It is the endpoint the
	// cross-cloud proof POSTs a GCP-minted JWT-SVID to, and a separate VM would
	// add cost without changing what is demonstrated.
	serveScript, err := renderForgeServeScript(forgeServeArgs{
		LocalTrustDomain:  args.TrustDomain,
		RemoteTrustDomain: args.PeerTrustDomain,
		BundleEndpointURL: fmt.Sprintf("https://%s:8443", gcpSPIREServerPrivateIP),
		RepoRef:           args.ForgeRepoRef,
	})
	if err != nil {
		return "", err
	}
	return script + serveScript, nil
}

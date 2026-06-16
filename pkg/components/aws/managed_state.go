package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/kms"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/secretsmanager"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ManagedStateArgs configures the AWS managed-state backend.
type ManagedStateArgs struct {
	Environment      string
	PrivateSubnetIDs pulumi.StringArrayOutput
	InternalSGID     pulumi.IDOutput
	DBPassword       pulumi.StringInput // expected to be a pulumi secret
}

// ManagedState provisions an RDS Postgres instance for the SPIRE DataStore
// plugin, a KMS CMK for the aws_kms KeyManager plugin, and a Secrets Manager
// secret placeholder for the SPIRE admin join token.
type ManagedState struct {
	pulumi.ResourceState

	DBEndpoint    pulumi.StringOutput
	KMSKeyARN     pulumi.StringOutput
	AdminSecretID pulumi.IDOutput
}

// NewManagedState wires RDS + KMS + Secrets Manager for the SPIRE server.
func NewManagedState(ctx *pulumi.Context, name string, args *ManagedStateArgs, opts ...pulumi.ResourceOption) (*ManagedState, error) {
	if args == nil {
		return nil, fmt.Errorf("args must not be nil")
	}
	component := &ManagedState{}
	if err := ctx.RegisterComponentResource("forge:aws:ManagedState", name, component, opts...); err != nil {
		return nil, err
	}
	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	subnetGroup, err := rds.NewSubnetGroup(ctx, namePrefix+"-spire-sng", &rds.SubnetGroupArgs{
		SubnetIds: args.PrivateSubnetIDs,
		Tags:      pulumi.StringMap{"Name": pulumi.String(namePrefix + "-spire-sng")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	db, err := rds.NewInstance(ctx, namePrefix+"-spire-db", &rds.InstanceArgs{
		Engine:               pulumi.String("postgres"),
		EngineVersion:        pulumi.String("15"),
		InstanceClass:        pulumi.String("db.t4g.micro"),
		AllocatedStorage:     pulumi.Int(20),
		DbSubnetGroupName:    subnetGroup.Name,
		VpcSecurityGroupIds:  pulumi.StringArray{args.InternalSGID.ToStringOutput()},
		Username:             pulumi.String("spire"),
		Password:             args.DBPassword,
		DbName:               pulumi.String("spire"),
		SkipFinalSnapshot:    pulumi.Bool(true),
		PubliclyAccessible:   pulumi.Bool(false),
		StorageEncrypted:     pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name":            pulumi.String(namePrefix + "-spire-db"),
			"forge:component": pulumi.String("spire-datastore"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	// Allow SPIRE server to reach RDS on 5432 within the VPC.
	_, err = ec2.NewSecurityGroupRule(ctx, namePrefix+"-sg-rule-rds-ingress", &ec2.SecurityGroupRuleArgs{
		Type:            pulumi.String("ingress"),
		SecurityGroupId: args.InternalSGID.ToStringOutput(),
		FromPort:        pulumi.Int(5432),
		ToPort:          pulumi.Int(5432),
		Protocol:        pulumi.String("tcp"),
		Self:            pulumi.Bool(true),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	key, err := kms.NewKey(ctx, namePrefix+"-spire-key", &kms.KeyArgs{
		Description:          pulumi.String("SPIRE server KeyManager CMK"),
		KeyUsage:             pulumi.String("SIGN_VERIFY"),
		CustomerMasterKeySpec: pulumi.String("ECC_NIST_P256"),
		Tags: pulumi.StringMap{"Name": pulumi.String(namePrefix + "-spire-key")},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	secret, err := secretsmanager.NewSecret(ctx, namePrefix+"-spire-admin", &secretsmanager.SecretArgs{
		Name:        pulumi.String(namePrefix + "-spire-admin"),
		Description: pulumi.String("SPIRE admin join token (populated out-of-band)"),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.DBEndpoint = db.Endpoint
	component.KMSKeyARN = key.Arn
	component.AdminSecretID = secret.ID()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"dbEndpoint":    db.Endpoint,
		"kmsKeyArn":     key.Arn,
		"adminSecretId": secret.ID(),
	}); err != nil {
		return nil, err
	}
	return component, nil
}

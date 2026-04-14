package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/kms"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/sql"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ManagedStateArgs configures the GCP managed-state backend.
type ManagedStateArgs struct {
	Environment string
	Region      string
}

// ManagedState provisions Cloud SQL (Postgres) for the SPIRE DataStore plugin,
// a Cloud KMS keyring + key for the gcp_kms KeyManager plugin, and a Secret
// Manager secret placeholder for the SPIRE admin join token.
type ManagedState struct {
	pulumi.ResourceState

	SQLConnectionName pulumi.StringOutput
	KMSKeyID          pulumi.StringOutput
	AdminSecretID     pulumi.StringOutput
}

// NewManagedState wires Cloud SQL + KMS + Secret Manager for the SPIRE server.
func NewManagedState(ctx *pulumi.Context, name string, args *ManagedStateArgs, opts ...pulumi.ResourceOption) (*ManagedState, error) {
	component := &ManagedState{}
	if err := ctx.RegisterComponentResource("forge:gcp:ManagedState", name, component, opts...); err != nil {
		return nil, err
	}
	parentOpt := pulumi.Parent(component)
	namePrefix := fmt.Sprintf("forge-%s", args.Environment)

	sqlInstance, err := sql.NewDatabaseInstance(ctx, namePrefix+"-spire-sql", &sql.DatabaseInstanceArgs{
		DatabaseVersion:    pulumi.String("POSTGRES_15"),
		Region:             pulumi.String(args.Region),
		DeletionProtection: pulumi.Bool(false),
		Settings: &sql.DatabaseInstanceSettingsArgs{
			Tier: pulumi.String("db-f1-micro"),
			IpConfiguration: &sql.DatabaseInstanceSettingsIpConfigurationArgs{
				Ipv4Enabled: pulumi.Bool(true),
			},
			BackupConfiguration: &sql.DatabaseInstanceSettingsBackupConfigurationArgs{
				Enabled:                     pulumi.Bool(true),
				PointInTimeRecoveryEnabled:  pulumi.Bool(false),
			},
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	_, err = sql.NewDatabase(ctx, namePrefix+"-spire-db", &sql.DatabaseArgs{
		Instance: sqlInstance.Name,
		Name:     pulumi.String("spire"),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	keyring, err := kms.NewKeyRing(ctx, namePrefix+"-spire-kr", &kms.KeyRingArgs{
		Location: pulumi.String(args.Region),
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	key, err := kms.NewCryptoKey(ctx, namePrefix+"-spire-key", &kms.CryptoKeyArgs{
		KeyRing: keyring.ID(),
		Purpose: pulumi.String("ASYMMETRIC_SIGN"),
		VersionTemplate: &kms.CryptoKeyVersionTemplateArgs{
			Algorithm: pulumi.String("EC_SIGN_P256_SHA256"),
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	secret, err := secretmanager.NewSecret(ctx, namePrefix+"-spire-admin", &secretmanager.SecretArgs{
		SecretId: pulumi.String(namePrefix + "-spire-admin"),
		Replication: &secretmanager.SecretReplicationArgs{
			Auto: &secretmanager.SecretReplicationAutoArgs{},
		},
	}, parentOpt)
	if err != nil {
		return nil, err
	}

	component.SQLConnectionName = sqlInstance.ConnectionName
	component.KMSKeyID = key.ID().ToStringOutput()
	component.AdminSecretID = secret.ID().ToStringOutput()

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"sqlConnectionName": sqlInstance.ConnectionName,
		"kmsKeyId":          key.ID(),
		"adminSecretId":     secret.ID(),
	}); err != nil {
		return nil, err
	}
	return component, nil
}

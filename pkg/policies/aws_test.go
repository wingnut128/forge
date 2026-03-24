package policies

import "testing"

// --- Helpers ---

func validAWSVPCInput() AWSVPCPolicyInput {
	return AWSVPCPolicyInput{
		Environment:    "dev",
		ResourceName:   "forge-dev-vpc",
		CustomVPC:      true,
		MultiAZ:        true,
		PrivateSubnets: true,
	}
}

func validEKSInput() EKSPolicyInput {
	return EKSPolicyInput{
		Environment:      "dev",
		ResourceName:     "forge-dev-eks",
		PrivateEndpoint:  true,
		EncryptedSecrets: true,
		LoggingEnabled:   true,
	}
}

func validSPIREOIDCInput() SPIREOIDCPolicyInput {
	return SPIREOIDCPolicyInput{
		Environment:    "dev",
		ResourceName:   "forge-dev-spire-oidc-gcp",
		OIDCIssuerSet:  true,
		TrustDomainSet: true,
	}
}

// --- AWS VPC tests ---

func TestCheckAWSVPC_AllValid(t *testing.T) {
	r := &Result{}
	CheckAWSVPC(validAWSVPCInput(), r)
	if len(r.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(r.Violations), r.Violations)
	}
}

func TestCheckAWSVPC_CustomVPCRequired(t *testing.T) {
	input := validAWSVPCInput()
	input.CustomVPC = false
	r := &Result{}
	CheckAWSVPC(input, r)
	assertHasViolation(t, r, "aws-vpc-custom", Mandatory)
}

func TestCheckAWSVPC_MultiAZRequired(t *testing.T) {
	input := validAWSVPCInput()
	input.MultiAZ = false
	r := &Result{}
	CheckAWSVPC(input, r)
	assertHasViolation(t, r, "aws-vpc-multi-az", Mandatory)
}

func TestCheckAWSVPC_PrivateSubnetsRequired(t *testing.T) {
	input := validAWSVPCInput()
	input.PrivateSubnets = false
	r := &Result{}
	CheckAWSVPC(input, r)
	assertHasViolation(t, r, "aws-vpc-private-subnets", Mandatory)
}

func TestCheckAWSVPC_BadNaming(t *testing.T) {
	input := validAWSVPCInput()
	input.ResourceName = "my-vpc"
	r := &Result{}
	CheckAWSVPC(input, r)
	assertHasViolation(t, r, "naming-convention", Mandatory)
}

// --- EKS tests ---

func TestCheckEKS_AllValid(t *testing.T) {
	r := &Result{}
	CheckEKS(validEKSInput(), r)
	if len(r.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(r.Violations), r.Violations)
	}
}

func TestCheckEKS_PrivateEndpointRequired(t *testing.T) {
	input := validEKSInput()
	input.PrivateEndpoint = false
	r := &Result{}
	CheckEKS(input, r)
	assertHasViolation(t, r, "eks-private-endpoint", Mandatory)
}

func TestCheckEKS_EncryptedSecretsRequired(t *testing.T) {
	input := validEKSInput()
	input.EncryptedSecrets = false
	r := &Result{}
	CheckEKS(input, r)
	assertHasViolation(t, r, "eks-encrypted-secrets", Mandatory)
}

func TestCheckEKS_LoggingIsAdvisory(t *testing.T) {
	input := validEKSInput()
	input.LoggingEnabled = false
	r := &Result{}
	CheckEKS(input, r)
	assertHasViolation(t, r, "eks-logging", Advisory)
}

func TestCheckEKS_BadNaming(t *testing.T) {
	input := validEKSInput()
	input.ResourceName = "cluster"
	r := &Result{}
	CheckEKS(input, r)
	assertHasViolation(t, r, "naming-convention", Mandatory)
}

// --- SPIRE OIDC tests ---

func TestCheckSPIREOIDC_AllValid(t *testing.T) {
	r := &Result{}
	CheckSPIREOIDC(validSPIREOIDCInput(), r)
	if len(r.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(r.Violations), r.Violations)
	}
}

func TestCheckSPIREOIDC_OIDCIssuerRequired(t *testing.T) {
	input := validSPIREOIDCInput()
	input.OIDCIssuerSet = false
	r := &Result{}
	CheckSPIREOIDC(input, r)
	assertHasViolation(t, r, "spire-oidc-issuer", Mandatory)
}

func TestCheckSPIREOIDC_TrustDomainRequired(t *testing.T) {
	input := validSPIREOIDCInput()
	input.TrustDomainSet = false
	r := &Result{}
	CheckSPIREOIDC(input, r)
	assertHasViolation(t, r, "spire-oidc-trust-domain", Mandatory)
}

func TestCheckSPIREOIDC_BadNaming(t *testing.T) {
	input := validSPIREOIDCInput()
	input.ResourceName = "oidc-provider"
	r := &Result{}
	CheckSPIREOIDC(input, r)
	assertHasViolation(t, r, "naming-convention", Mandatory)
}

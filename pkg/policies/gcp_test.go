package policies

import "testing"

// --- Helpers ---

func validGKEInput() GKEPolicyInput {
	return GKEPolicyInput{
		Environment:       "dev",
		ResourceName:      "forge-dev-gke",
		PrivateCluster:    true,
		WorkloadIdentity:  true,
		BinaryAuthEnabled: true,
		NetworkPolicy:     true,
		SecureBoot:        true,
		IntegrityMonitor:  true,
		AutoRepair:        true,
		AutoUpgrade:       true,
	}
}

func validNetworkInput() NetworkPolicyInput {
	return NetworkPolicyInput{
		Environment:         "dev",
		ResourceName:        "forge-dev-vpc",
		CustomSubnetMode:    true,
		PrivateGoogleAccess: true,
	}
}

func validWIFInput() WorkloadIdentityPolicyInput {
	return WorkloadIdentityPolicyInput{
		Environment:        "dev",
		ResourceName:       "forge-dev-spiffe-pool",
		AttributeCondition: "assertion.sub.startsWith('spiffe://remote.example.com/')",
		HasAudiences:       true,
	}
}

func assertHasViolation(t *testing.T, r *Result, policy string, sev Severity) {
	t.Helper()
	for _, v := range r.Violations {
		if v.Policy == policy && v.Severity == sev {
			return
		}
	}
	t.Errorf("expected violation %q with severity %s, got: %+v", policy, sev, r.Violations)
}

// --- GKE tests ---

func TestCheckGKE_AllValid(t *testing.T) {
	r := &Result{}
	CheckGKE(validGKEInput(), r)
	if len(r.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(r.Violations), r.Violations)
	}
}

func TestCheckGKE_PrivateClusterRequired(t *testing.T) {
	input := validGKEInput()
	input.PrivateCluster = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-private-cluster", Mandatory)
}

func TestCheckGKE_WorkloadIdentityRequired(t *testing.T) {
	input := validGKEInput()
	input.WorkloadIdentity = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-workload-identity", Mandatory)
}

func TestCheckGKE_BinaryAuthRequired(t *testing.T) {
	input := validGKEInput()
	input.BinaryAuthEnabled = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-binary-authorization", Mandatory)
}

func TestCheckGKE_NetworkPolicyRequired(t *testing.T) {
	input := validGKEInput()
	input.NetworkPolicy = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-network-policy", Mandatory)
}

func TestCheckGKE_SecureBootRequired(t *testing.T) {
	input := validGKEInput()
	input.SecureBoot = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-shielded-secure-boot", Mandatory)
}

func TestCheckGKE_IntegrityMonitorRequired(t *testing.T) {
	input := validGKEInput()
	input.IntegrityMonitor = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-shielded-integrity", Mandatory)
}

func TestCheckGKE_AutoRepairIsAdvisory(t *testing.T) {
	input := validGKEInput()
	input.AutoRepair = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-auto-repair", Advisory)
}

func TestCheckGKE_AutoUpgradeIsAdvisory(t *testing.T) {
	input := validGKEInput()
	input.AutoUpgrade = false
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "gke-auto-upgrade", Advisory)
}

func TestCheckGKE_BadNaming(t *testing.T) {
	input := validGKEInput()
	input.ResourceName = "my-cluster"
	r := &Result{}
	CheckGKE(input, r)
	assertHasViolation(t, r, "naming-convention", Mandatory)
}

// --- Network tests ---

func TestCheckNetwork_AllValid(t *testing.T) {
	r := &Result{}
	CheckNetwork(validNetworkInput(), r)
	if len(r.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(r.Violations), r.Violations)
	}
}

func TestCheckNetwork_CustomSubnetRequired(t *testing.T) {
	input := validNetworkInput()
	input.CustomSubnetMode = false
	r := &Result{}
	CheckNetwork(input, r)
	assertHasViolation(t, r, "network-custom-subnet", Mandatory)
}

func TestCheckNetwork_PrivateGoogleAccessRequired(t *testing.T) {
	input := validNetworkInput()
	input.PrivateGoogleAccess = false
	r := &Result{}
	CheckNetwork(input, r)
	assertHasViolation(t, r, "network-private-google-access", Mandatory)
}

func TestCheckNetwork_BadNaming(t *testing.T) {
	input := validNetworkInput()
	input.ResourceName = "default-vpc"
	r := &Result{}
	CheckNetwork(input, r)
	assertHasViolation(t, r, "naming-convention", Mandatory)
}

// --- WorkloadIdentity tests ---

func TestCheckWorkloadIdentity_AllValid(t *testing.T) {
	r := &Result{}
	CheckWorkloadIdentity(validWIFInput(), r)
	if len(r.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(r.Violations), r.Violations)
	}
}

func TestCheckWorkloadIdentity_EmptyCondition(t *testing.T) {
	input := validWIFInput()
	input.AttributeCondition = ""
	r := &Result{}
	CheckWorkloadIdentity(input, r)
	assertHasViolation(t, r, "wif-attribute-condition", Mandatory)
}

func TestCheckWorkloadIdentity_NoAudiences(t *testing.T) {
	input := validWIFInput()
	input.HasAudiences = false
	r := &Result{}
	CheckWorkloadIdentity(input, r)
	assertHasViolation(t, r, "wif-allowed-audiences", Mandatory)
}

func TestCheckWorkloadIdentity_BadNaming(t *testing.T) {
	input := validWIFInput()
	input.ResourceName = "spiffe-pool"
	r := &Result{}
	CheckWorkloadIdentity(input, r)
	assertHasViolation(t, r, "naming-convention", Mandatory)
}

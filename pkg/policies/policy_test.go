package policies

import "testing"

func TestResult_NoViolations(t *testing.T) {
	r := &Result{}
	if r.HasMandatory() {
		t.Error("empty result should not have mandatory violations")
	}
	if r.Error() != nil {
		t.Error("empty result should return nil error")
	}
}

func TestResult_AdvisoryOnly(t *testing.T) {
	r := &Result{}
	r.Add("test", Advisory, "just a warning")
	if r.HasMandatory() {
		t.Error("advisory-only should not count as mandatory")
	}
	if r.Error() != nil {
		t.Error("advisory-only should return nil error")
	}
	if len(r.Violations) != 1 {
		t.Errorf("got %d violations, want 1", len(r.Violations))
	}
}

func TestResult_MandatoryPresent(t *testing.T) {
	r := &Result{}
	r.Add("test", Mandatory, "must fix")
	if !r.HasMandatory() {
		t.Error("should detect mandatory violation")
	}
	if r.Error() == nil {
		t.Error("should return error for mandatory violation")
	}
	if r.MandatoryCount() != 1 {
		t.Errorf("MandatoryCount = %d, want 1", r.MandatoryCount())
	}
}

func TestResult_MixedSeverities(t *testing.T) {
	r := &Result{}
	r.Add("a", Advisory, "warning")
	r.Add("b", Mandatory, "blocker")
	r.Add("c", Advisory, "another warning")
	if r.MandatoryCount() != 1 {
		t.Errorf("MandatoryCount = %d, want 1", r.MandatoryCount())
	}
	if len(r.Violations) != 3 {
		t.Errorf("total = %d, want 3", len(r.Violations))
	}
}

func TestSeverity_String(t *testing.T) {
	if Advisory.String() != "advisory" {
		t.Errorf("Advisory.String() = %q", Advisory.String())
	}
	if Mandatory.String() != "mandatory" {
		t.Errorf("Mandatory.String() = %q", Mandatory.String())
	}
}

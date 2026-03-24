package policies

import "fmt"

// Severity classifies how a policy violation should be handled.
type Severity int

const (
	// Advisory logs a warning but does not block deployment.
	Advisory Severity = iota
	// Mandatory blocks deployment if violated.
	Mandatory
)

func (s Severity) String() string {
	switch s {
	case Advisory:
		return "advisory"
	case Mandatory:
		return "mandatory"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Violation records a single policy check failure.
type Violation struct {
	Policy   string
	Severity Severity
	Message  string
}

// Result collects violations from one or more policy checks.
type Result struct {
	Violations []Violation
}

// Add records a violation.
func (r *Result) Add(policy string, sev Severity, format string, args ...any) {
	r.Violations = append(r.Violations, Violation{
		Policy:   policy,
		Severity: sev,
		Message:  fmt.Sprintf(format, args...),
	})
}

// HasMandatory returns true if any mandatory violation exists.
func (r *Result) HasMandatory() bool {
	for _, v := range r.Violations {
		if v.Severity == Mandatory {
			return true
		}
	}
	return false
}

// MandatoryCount returns the number of mandatory violations.
func (r *Result) MandatoryCount() int {
	n := 0
	for _, v := range r.Violations {
		if v.Severity == Mandatory {
			n++
		}
	}
	return n
}

// Error returns a summary error if there are mandatory violations, nil otherwise.
func (r *Result) Error() error {
	m := r.MandatoryCount()
	if m == 0 {
		return nil
	}
	return fmt.Errorf("policy check failed: %d mandatory violation(s)", m)
}

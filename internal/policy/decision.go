package policy

// Decision represents the result of a policy check.
type Decision int

const (
	Allow Decision = iota
	Deny
	NeedsApproval
)

func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case NeedsApproval:
		return "needs_approval"
	default:
		return "unknown"
	}
}

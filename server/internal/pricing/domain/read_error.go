package domain

type ReadFailure struct{ Kind, Message string }

func (e *ReadFailure) Error() string { return e.Message }

const (
	InvalidQuery     = "invalid"
	Unavailable      = "unavailable"
	UserUnavailable  = "user"
	NotFound         = "not_found"
	RevisionMismatch = "revision"
)

func CheckRevision(expected, actual string) error {
	if expected != "" && expected != actual {
		return &ReadFailure{Kind: RevisionMismatch, Message: "Pricing catalog revision changed"}
	}
	return nil
}

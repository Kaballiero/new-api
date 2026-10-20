package domain

type EnrollmentErrors struct {
	FlowInvalid, FlowExpired, FlowConsumed, SetupInvalid func() error
}

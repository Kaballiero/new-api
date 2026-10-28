package domain

const MaxResultBytes = 8 << 20

type State string

const (
	Validating State = "validating"
	Running    State = "in_progress"
	Finalizing State = "finalizing"
	Cancelling State = "cancelling"
	Completed  State = "completed"
	Failed     State = "failed"
	Expired    State = "expired"
	Cancelled  State = "cancelled"
)

func (s State) Terminal() bool {
	return s == Completed || s == Failed || s == Expired || s == Cancelled
}

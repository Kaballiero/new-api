package domain

type SubmissionUpdate struct {
	ID        string
	State     State
	LastError *string
}
type PollUpdate struct {
	State                            State
	OutputFileID, ErrorFileID, Usage string
	LastError                        *string
}

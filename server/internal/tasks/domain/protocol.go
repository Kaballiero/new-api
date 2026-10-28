package domain

// ProtocolSubmission is the durable gateway result consumed by task-plugin observation policy.
// OwnerID is the identity against which the gateway admitted the submission.
type ProtocolSubmission struct {
	Task    *Task
	OwnerID int
	Model   string
}

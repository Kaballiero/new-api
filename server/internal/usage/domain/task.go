package domain

// TaskIdentity is the credential-free provenance attached to task usage rows.
type TaskIdentity struct {
	TaskID, UpstreamTaskID, NodeName string
	Plugin                           *Plugin
}

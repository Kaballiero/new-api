package domain

const (
	SystemInstanceStatusOnline = "online"
	SystemInstanceStatusStale  = "stale"

	SystemInstanceStaleAfterSeconds int64 = 90
)

func InstanceStatus(lastSeen, now int64) string {
	if now-lastSeen > SystemInstanceStaleAfterSeconds {
		return SystemInstanceStatusStale
	}
	return SystemInstanceStatusOnline
}

type Instance struct {
	NodeName              string
	StartedAt, LastSeenAt int64
	Info                  string
}

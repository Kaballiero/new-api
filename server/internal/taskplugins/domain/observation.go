package domain

import (
	"errors"
	"time"
)

var ErrProtocolSubmission = errors.New("task protocol submission failed")
var ErrInvalidDurableOutcome = errors.New("invalid durable task protocol outcome")
var ErrObservationHandled = errors.New("task protocol wire outcome already handled")
var ErrRenderAdmission = errors.New("task protocol render admission timed out")

type ObservationTiming struct{ Submission, Observation, Load, Tick, Jitter, Heartbeat time.Duration }

func DebugTaskStatus(status string) string {
	switch status {
	case "SUBMITTED", "QUEUED", "IN_PROGRESS", "SUCCESS", "FAILURE":
		return status
	default:
		return "unknown"
	}
}

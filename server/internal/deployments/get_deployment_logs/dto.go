package get_deployment_logs

import (
	"time"
)

type GetLogsOptions struct {
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Level     string     `json:"level,omitempty"`  // filter by log level
	Stream    string     `json:"stream,omitempty"` // filter by stdout/stderr streams
	Limit     int        `json:"limit,omitempty"`  // max number of log entries
	Cursor    string     `json:"cursor,omitempty"` // pagination cursor
	Follow    bool       `json:"follow,omitempty"` // stream logs
}

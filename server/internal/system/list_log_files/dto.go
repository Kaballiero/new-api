package list_log_files

import (
	"time"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type LogFilesResponse struct {
	LogDir     string               `json:"log_dir"`
	Enabled    bool                 `json:"enabled"`
	FileCount  int                  `json:"file_count"`
	TotalSize  int64                `json:"total_size"`
	OldestTime *time.Time           `json:"oldest_time,omitempty"`
	NewestTime *time.Time           `json:"newest_time,omitempty"`
	Files      []domain.LogFileInfo `json:"files"`
}

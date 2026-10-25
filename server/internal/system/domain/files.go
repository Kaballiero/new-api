package domain

import "time"

type LogFileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type CleanupFilesResult struct {
	DeletedCount  int
	FreedBytes    int64
	FailedFiles   []string
	SelectedCount int
}

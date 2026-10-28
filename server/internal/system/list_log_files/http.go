package list_log_files

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/domain"

	"github.com/gin-gonic/gin"
)

type Files interface {
	Directory() string
	List() ([]domain.LogFileInfo, error)
}
type Handler struct{ files Files }

func NewHandler(files Files) Handler { return Handler{files: files} }
func (h Handler) Handle(c *gin.Context) {
	if h.files.Directory() == "" {
		common.ApiSuccess(c, LogFilesResponse{Enabled: false})
		return
	}
	files, err := h.files.List()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var totalSize int64
	var oldest, newest time.Time
	for i, f := range files {
		totalSize += f.Size
		if i == 0 || f.ModTime.Before(oldest) {
			oldest = f.ModTime
		}
		if i == 0 || f.ModTime.After(newest) {
			newest = f.ModTime
		}
	}
	resp := LogFilesResponse{
		LogDir:    h.files.Directory(),
		Enabled:   true,
		FileCount: len(files),
		TotalSize: totalSize,
		Files:     files,
	}
	if len(files) > 0 {
		resp.OldestTime = &oldest
		resp.NewestTime = &newest
	}
	common.ApiSuccess(c, resp)
}

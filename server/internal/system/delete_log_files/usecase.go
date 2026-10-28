package delete_log_files

import (
	"time"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type Files interface {
	List() ([]domain.LogFileInfo, error)
	Directory() string
	ActivePath() string
	Path(string, string) string
	Remove(string) error
}
type Usecase struct {
	files Files
	now   func() time.Time
}

func NewUsecase(files Files, now func() time.Time) Usecase { return Usecase{files: files, now: now} }

type Input struct {
	Mode  string
	Value int
}

func (u Usecase) Execute(input Input) (domain.CleanupFilesResult, error) {
	mode, value := input.Mode, input.Value
	files, err := u.files.List()
	if err != nil {
		return domain.CleanupFilesResult{}, err
	}

	activeLogPath := u.files.ActivePath()
	var toDelete []domain.LogFileInfo

	switch mode {
	case "by_count":
		for i, f := range files {
			if i < value {
				continue
			}
			fullPath := u.files.Path(u.files.Directory(), f.Name)
			if fullPath == activeLogPath {
				continue
			}
			toDelete = append(toDelete, f)
		}
	case "by_days":
		cutoff := u.now().AddDate(0, 0, -value)
		for _, f := range files {
			if f.ModTime.Before(cutoff) {
				fullPath := u.files.Path(u.files.Directory(), f.Name)
				if fullPath == activeLogPath {
					continue
				}
				toDelete = append(toDelete, f)
			}
		}
	}

	var deletedCount int
	var freedBytes int64
	var failedFiles []string
	for _, f := range toDelete {
		fullPath := u.files.Path(u.files.Directory(), f.Name)
		if err := u.files.Remove(fullPath); err != nil {
			failedFiles = append(failedFiles, f.Name)
			continue
		}
		deletedCount++
		freedBytes += f.Size
	}

	return domain.CleanupFilesResult{DeletedCount: deletedCount, FreedBytes: freedBytes, FailedFiles: failedFiles, SelectedCount: len(toDelete)}, nil
}

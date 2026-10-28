package system

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/logger"
)

type LogFiles struct{}

func (LogFiles) Directory() string            { return *common.LogDir }
func (LogFiles) ActivePath() string           { return logger.GetCurrentLogPath() }
func (LogFiles) Path(dir, name string) string { return filepath.Join(dir, name) }
func (LogFiles) Remove(path string) error     { return os.Remove(path) }
func (LogFiles) List() ([]domain.LogFileInfo, error) {
	if *common.LogDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(*common.LogDir)
	if err != nil {
		return nil, err
	}
	var files []domain.LogFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "oneapi-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, domain.LogFileInfo{
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name > files[j].Name
	})
	return files, nil
}

package system

import (
	"os"
	"runtime"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/get_performance_stats"
)

type Performance struct{}

func (Performance) Stats() get_performance_stats.PerformanceStats {
	cacheStats := common.GetDiskCacheStats()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	diskCacheInfo := diskCacheInfo()

	diskConfig := common.GetDiskCacheConfig()
	monitorConfig := common.GetPerformanceMonitorConfig()
	config := get_performance_stats.PerformanceConfig{
		DiskCacheEnabled:       diskConfig.Enabled,
		DiskCacheThresholdMB:   diskConfig.ThresholdMB,
		DiskCacheMaxSizeMB:     diskConfig.MaxSizeMB,
		DiskCachePath:          diskConfig.Path,
		IsRunningInContainer:   common.IsRunningInContainer(),
		MonitorEnabled:         monitorConfig.Enabled,
		MonitorCPUThreshold:    monitorConfig.CPUThreshold,
		MonitorMemoryThreshold: monitorConfig.MemoryThreshold,
		MonitorDiskThreshold:   monitorConfig.DiskThreshold,
	}

	systemStatus := common.GetSystemStatus()
	diskSpaceInfo := common.DiskSpaceInfo{
		UsedPercent: systemStatus.DiskUsage,
	}
	diskSpaceInfo = common.GetDiskSpaceInfo()

	stats := get_performance_stats.PerformanceStats{
		CacheStats: cacheStats,
		MemoryStats: get_performance_stats.MemoryStats{
			Alloc:        memStats.Alloc,
			TotalAlloc:   memStats.TotalAlloc,
			Sys:          memStats.Sys,
			NumGC:        memStats.NumGC,
			NumGoroutine: runtime.NumGoroutine(),
		},
		DiskCacheInfo: diskCacheInfo,
		DiskSpaceInfo: diskSpaceInfo,
		Config:        config,
	}

	return stats
}
func diskCacheInfo() get_performance_stats.DiskCacheInfo {
	dir := common.GetDiskCacheDir()

	info := get_performance_stats.DiskCacheInfo{
		Path:   dir,
		Exists: false,
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return info
	}

	info.Exists = true
	info.FileCount = 0
	info.TotalSize = 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info.FileCount++
		if fileInfo, err := entry.Info(); err == nil {
			info.TotalSize += fileInfo.Size()
		}
	}

	return info
}

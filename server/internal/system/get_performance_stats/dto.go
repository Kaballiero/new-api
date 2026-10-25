package get_performance_stats

import "github.com/QuantumNous/new-api/common"

type PerformanceStats struct {
	CacheStats    common.DiskCacheStats `json:"cache_stats"`
	MemoryStats   MemoryStats           `json:"memory_stats"`
	DiskCacheInfo DiskCacheInfo         `json:"disk_cache_info"`
	DiskSpaceInfo common.DiskSpaceInfo  `json:"disk_space_info"`
	Config        PerformanceConfig     `json:"config"`
}

type MemoryStats struct {
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"num_gc"`
	NumGoroutine int    `json:"num_goroutine"`
}

type DiskCacheInfo struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	FileCount int    `json:"file_count"`
	TotalSize int64  `json:"total_size"`
}

type PerformanceConfig struct {
	DiskCacheEnabled     bool   `json:"disk_cache_enabled"`
	DiskCacheThresholdMB int    `json:"disk_cache_threshold_mb"`
	DiskCacheMaxSizeMB   int    `json:"disk_cache_max_size_mb"`
	DiskCachePath        string `json:"disk_cache_path"`
	IsRunningInContainer bool   `json:"is_running_in_container"`

	MonitorEnabled         bool `json:"monitor_enabled"`
	MonitorCPUThreshold    int  `json:"monitor_cpu_threshold"`
	MonitorMemoryThreshold int  `json:"monitor_memory_threshold"`
	MonitorDiskThreshold   int  `json:"monitor_disk_threshold"`
}

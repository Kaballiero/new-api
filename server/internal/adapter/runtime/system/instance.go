package system

import (
	"os"
	"runtime"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/domain"
)

type InstanceFacts struct{}

func (InstanceFacts) Identity() (domain.NodeIdentity, string, error) {
	v := common.GetNodeIdentity()
	h, e := os.Hostname()
	return domain.NodeIdentity{Name: v.Name, Source: v.Source, ManuallyConfigured: v.ManuallyConfigured, ShouldConfigureManually: v.ShouldConfigureManually}, h, e
}
func (InstanceFacts) Snapshot(identity domain.NodeIdentity, hostname string) domain.SystemInstanceInfo {
	systemStatus := common.GetSystemStatus()
	diskInfo := common.GetDiskSpaceInfo()
	info := domain.SystemInstanceInfo{
		SchemaVersion: 1,
		Node:          identity,
		Role: domain.SystemInstanceRoleInfo{
			IsMaster: common.IsMasterNode,
		},
		Runtime: domain.SystemInstanceRuntimeInfo{
			Version:   common.Version,
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			StartedAt: common.StartTime,
		},
		Host: domain.SystemInstanceHostInfo{
			Hostname: hostname,
		},
		Resources: domain.SystemInstanceResources{
			CPU: domain.SystemInstanceResourceUsage{
				UsagePercent: systemStatus.CPUUsage,
			},
			Memory: domain.SystemInstanceResourceUsage{
				UsagePercent: systemStatus.MemoryUsage,
			},
			Storage: domain.SystemInstanceStorageMetrics{
				TotalBytes:  diskInfo.Total,
				UsedBytes:   diskInfo.Used,
				FreeBytes:   diskInfo.Free,
				UsedPercent: diskInfo.UsedPercent,
			},
		},
	}
	return info
}

package taskview

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
)

// TaskExecutionSnapshotFromContext captures immutable request and plugin
// provenance at submission time. It never copies plugin source or payloads.
func TaskExecutionSnapshotFromContext(ctx *gin.Context) *model.TaskExecutionSnapshot {
	if ctx == nil {
		return nil
	}
	snapshot := &model.TaskExecutionSnapshot{
		RequestID: ctx.GetString(common.RequestIdKey),
	}
	if ctx.Request != nil && ctx.Request.URL != nil {
		snapshot.RequestPath = ctx.Request.URL.Path
	}

	pinnedValue, exists := ctx.Get(pluginruntime.ContextKeyPinnedPlugin)
	if exists {
		pinned, ok := pinnedValue.(pluginruntime.PinnedPlugin)
		if ok && pinned.Plugin != nil {
			generation := uint64(0)
			if pinned.Generation != nil {
				generation = pinned.Generation.Number
			}
			meta := pinned.Plugin.Meta
			snapshot.TaskPlugin = &model.TaskPluginSnapshot{
				Key:     meta.Key,
				Name:    meta.Name,
				Version: meta.Version,
				Author: &model.TaskPluginAuthorSnapshot{
					Name: meta.Author.Name,
					URL:  meta.Author.URL,
				},
				APIVersion: meta.APIVersion,
				Generation: generation,
			}
		}
	}

	if snapshot.RequestID == "" && snapshot.RequestPath == "" && snapshot.TaskPlugin == nil {
		return nil
	}
	return snapshot
}

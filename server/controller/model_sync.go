package controller

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/preview_metadata_sync"
	"github.com/gin-gonic/gin"
)

func SyncUpstreamPreview(c *gin.Context) { catalogHTTP().PreviewMetadataSync.Handle(c) }
func SyncUpstreamModels(c *gin.Context)  { catalogHTTP().ApplyMetadataSync.Handle(c) }

type SyncUpstreamSource = domain.SyncUpstreamSource
type SyncUpstreamPreviewResponse = preview_metadata_sync.SyncUpstreamPreviewResponse

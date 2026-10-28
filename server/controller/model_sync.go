package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/preview_metadata_sync"

	"github.com/gin-gonic/gin"
)

func SyncUpstreamPreview(c *gin.Context) { httpapi.Catalog().PreviewMetadataSync.Handle(c) }

func SyncUpstreamModels(c *gin.Context) { httpapi.Catalog().ApplyMetadataSync.Handle(c) }

type SyncUpstreamSource = domain.SyncUpstreamSource

type SyncUpstreamPreviewResponse = preview_metadata_sync.SyncUpstreamPreviewResponse

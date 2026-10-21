package controller

import (
	"github.com/QuantumNous/new-api/internal/adapter/catalog/upstream"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/preview_metadata_sync"
	"github.com/gin-gonic/gin"
	"net/http"
)

func SyncUpstreamPreview(c *gin.Context) { catalogHTTP().PreviewMetadataSync.Handle(c) }
func SyncUpstreamModels(c *gin.Context)  { catalogHTTP().ApplyMetadataSync.Handle(c) }

type SyncUpstreamSource = domain.SyncUpstreamSource
type SyncUpstreamPreviewResponse = preview_metadata_sync.SyncUpstreamPreviewResponse
type metadataSyncCandidate = domain.MetadataSyncCandidate
type metadataSyncField = domain.MetadataSyncField
type upstreamModel = upstream.Model
type upstreamVendor = upstream.Vendor

func normalizeLocale(locale string) (string, bool) { return domain.NormalizeLocale(locale) }
func newHTTPClient() *http.Client                  { return upstream.NewHTTPClient() }

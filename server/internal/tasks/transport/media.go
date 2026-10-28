package transport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/gin-gonic/gin"
)

func MediaClientHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, 4)
	for _, name := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			result[name] = value
		}
	}
	return result
}

func WriteVideoError(c *gin.Context, status int, kind, message string) {
	c.Header("Cache-Control", "private, no-store")
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": kind}})
}
func WriteArtifactError(c *gin.Context, capability bool, status int, code, message string) {
	c.Header("Cache-Control", "private, no-store")
	if capability {
		status = http.StatusNotFound
		code = "artifact_not_found"
		message = i18n.T(c, i18n.MsgTasksArtifactNotFound)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.JSON(status, gin.H{"success": false, "code": code, "message": message})
		return
	}
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": code, "code": code}})
}
func WriteProjectionError(c *gin.Context, capability bool, err error) {
	if errors.Is(err, domain.ErrArtifactPluginUnavailable) {
		WriteArtifactError(c, capability, http.StatusServiceUnavailable, "artifact_plugin_unavailable", i18n.T(c, i18n.MsgTasksArtifactPreviewUnavailable))
		return
	}
	WriteArtifactError(c, capability, http.StatusInternalServerError, "artifact_plugin_error", i18n.T(c, i18n.MsgTasksArtifactPreviewFailed))
}
func WriteMediaError(c *gin.Context, capability bool, err error, logError func(error)) {
	if c.Writer.Written() {
		logError(err)
		return
	}
	var proxyErr *domain.MediaError
	if !errors.As(err, &proxyErr) {
		proxyErr = &domain.MediaError{
			Status:  http.StatusBadGateway,
			Code:    "artifact_upstream_error",
			Message: "Failed to fetch artifact content", MessageID: i18n.MsgTasksArtifactFetchFailed,
			Cause: err,
		}
	}
	c.Header("Cache-Control", "private, no-store")
	message := proxyErr.Message
	if proxyErr.MessageID != "" {
		message = i18n.T(c, proxyErr.MessageID, proxyErr.MessageData)
	}
	WriteArtifactError(c, capability, proxyErr.Status, proxyErr.Code, message)
}

func ReadTaskAccess(c *gin.Context, capability bool) domain.TaskAccess {
	return domain.TaskAccess{
		Capability: capability,
		UserID:     c.GetInt("id"),
		TokenID:    c.GetInt("token_id"),
		Admin:      c.GetInt("role") >= common.RoleAdminUser,
	}
}
func WriteTaskError(c *gin.Context, capability bool, err error) {
	status, code, message := http.StatusInternalServerError, "artifact_plugin_error", i18n.T(c, i18n.MsgTasksArtifactPreviewFailed)
	switch {
	case errors.Is(err, domain.ErrTaskQuery):
		code = "artifact_internal_error"
		message = i18n.T(c, i18n.MsgTasksQueryTaskFailed)
	case errors.Is(err, domain.ErrArtifactNotFound):
		status = http.StatusNotFound
		code = "artifact_not_found"
		message = i18n.T(c, i18n.MsgTasksArtifactNotFound)
	case errors.Is(err, domain.ErrArtifactsNotReady):
		status = http.StatusConflict
		code = "artifact_not_ready"
		message = i18n.T(c, i18n.MsgTasksArtifactsNotReady)
	case errors.Is(err, domain.ErrArtifactStorageUnavailable):
		status = http.StatusServiceUnavailable
		code = "artifact_storage_unavailable"
		message = i18n.T(c, i18n.MsgTasksArtifactStorageUnavailable)
	case errors.Is(err, domain.ErrArtifactURL):
		code = "artifact_url_error"
		message = i18n.T(c, i18n.MsgTasksArtifactURLFailed)
	case errors.Is(err, domain.ErrArtifactContentUnavailable):
		status = http.StatusServiceUnavailable
		code = "artifact_plugin_unavailable"
		message = i18n.T(c, i18n.MsgTasksArtifactContentUnavailable)
	case errors.Is(err, domain.ErrArtifactContent):
		code = "artifact_plugin_error"
		message = i18n.T(c, i18n.MsgTasksArtifactContentFailed)
	case errors.Is(err, domain.ErrArtifactGone):
		status = http.StatusGone
		code = "artifact_gone"
		message = i18n.T(c, i18n.MsgTasksArtifactGone)
	default:
		WriteProjectionError(c, capability, err)
		return
	}
	WriteArtifactError(c, capability, status, code, message)
}

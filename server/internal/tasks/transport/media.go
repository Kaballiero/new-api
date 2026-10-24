package transport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
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
		message = "Task or artifact not found"
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.JSON(status, gin.H{"success": false, "code": code, "message": message})
		return
	}
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": code, "code": code}})
}
func WriteProjectionError(c *gin.Context, capability bool, err error) {
	if errors.Is(err, domain.ErrArtifactPluginUnavailable) {
		WriteArtifactError(c, capability, http.StatusServiceUnavailable, "artifact_plugin_unavailable", "Artifact preview plugin is unavailable")
		return
	}
	WriteArtifactError(c, capability, http.StatusInternalServerError, "artifact_plugin_error", "Artifact preview plugin failed")
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
			Message: "Failed to fetch artifact content",
			Cause:   err,
		}
	}
	c.Header("Cache-Control", "private, no-store")
	WriteArtifactError(c, capability, proxyErr.Status, proxyErr.Code, proxyErr.Message)
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
	status, code, message := http.StatusInternalServerError, "artifact_plugin_error", "Artifact preview plugin failed"
	switch {
	case errors.Is(err, domain.ErrTaskQuery):
		code = "artifact_internal_error"
		message = "Failed to query task"
	case errors.Is(err, domain.ErrArtifactNotFound):
		status = http.StatusNotFound
		code = "artifact_not_found"
		message = "Task or artifact not found"
	case errors.Is(err, domain.ErrArtifactsNotReady):
		status = http.StatusConflict
		code = "artifact_not_ready"
		message = "Task artifacts are not ready"
	case errors.Is(err, domain.ErrArtifactStorageUnavailable):
		status = http.StatusServiceUnavailable
		code = "artifact_storage_unavailable"
		message = "Task artifact storage is unavailable"
	case errors.Is(err, domain.ErrArtifactURL):
		code = "artifact_url_error"
		message = "Failed to build artifact content URL"
	case errors.Is(err, domain.ErrArtifactContentUnavailable):
		status = http.StatusServiceUnavailable
		code = "artifact_plugin_unavailable"
		message = "Artifact content plugin is unavailable"
	case errors.Is(err, domain.ErrArtifactContent):
		code = "artifact_plugin_error"
		message = "Artifact content plugin failed"
	case errors.Is(err, domain.ErrArtifactGone):
		status = http.StatusGone
		code = "artifact_gone"
		message = "Artifact content is no longer available"
	default:
		WriteProjectionError(c, capability, err)
		return
	}
	WriteArtifactError(c, capability, status, code, message)
}

package transport

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/gin-gonic/gin"
)

func Key(c *gin.Context, settings func() domain.Settings) (string, bool) {
	key, err := settings().Key()
	if err != nil {
		common.ApiErrorMsg(c, "io.net model deployment is not enabled or api key missing")
		return "", false
	}
	return key, true
}
func Provider[T any](c *gin.Context, settings func() domain.Settings, factory func(string) T) (T, bool) {
	key, ok := Key(c, settings)
	if !ok {
		var zero T
		return zero, false
	}
	return factory(key), true
}
func Error(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNameEmpty):
		common.ApiErrorMsg(c, "deployment name cannot be empty")
	case errors.Is(err, domain.ErrNameUnavailable):
		common.ApiErrorMsg(c, "deployment name is not available, please choose a different name")
	default:
		common.ApiError(c, err)
	}
}
func RequireDeploymentID(c *gin.Context) (string, bool) {
	deploymentID := strings.TrimSpace(c.Param("id"))
	if deploymentID == "" {
		common.ApiErrorMsg(c, "deployment ID is required")
		return "", false
	}
	return deploymentID, true
}

func RequireContainerID(c *gin.Context) (string, bool) {
	containerID := strings.TrimSpace(c.Param("container_id"))
	if containerID == "" {
		common.ApiErrorMsg(c, "container ID is required")
		return "", false
	}
	return containerID, true
}

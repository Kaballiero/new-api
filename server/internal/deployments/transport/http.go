package transport

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/deployments/domain"

	"github.com/gin-gonic/gin"
)

func Key(c *gin.Context, settings func() domain.Settings) (string, bool) {
	key, err := settings().Key()
	if err != nil {
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageNotConfigured))
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
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageNameEmpty))
	case errors.Is(err, domain.ErrNameUnavailable):
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageNameUnavailable))
	default:
		common.ApiError(c, err)
	}
}
func RequireDeploymentID(c *gin.Context) (string, bool) {
	deploymentID := strings.TrimSpace(c.Param("id"))
	if deploymentID == "" {
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageIDRequired))
		return "", false
	}
	return deploymentID, true
}

func RequireContainerID(c *gin.Context) (string, bool) {
	containerID := strings.TrimSpace(c.Param("container_id"))
	if containerID == "" {
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageContainerIDRequired))
		return "", false
	}
	return containerID, true
}

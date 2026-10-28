package get_deployment_logs

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"

	"github.com/gin-gonic/gin"
)

type Provider interface {
	GetContainerLogsRaw(id, container string, options *GetLogsOptions) (string, error)
}
type HTTP struct {
	settings func() domain.Settings
	provider func(string) Provider
}

func NewHTTP(settings func() domain.Settings, provider func(string) Provider) HTTP {
	return HTTP{settings: settings, provider: provider}
}
func (h HTTP) Handle(c *gin.Context) {
	client, ok := transport.Provider(c, h.settings, h.provider)
	if !ok {
		return
	}

	deploymentID, ok := transport.RequireDeploymentID(c)
	if !ok {
		return
	}

	containerID := c.Query("container_id")
	if containerID == "" {
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageContainerParameter))
		return
	}
	level := c.Query("level")
	stream := c.Query("stream")
	cursor := c.Query("cursor")
	limitStr := c.Query("limit")
	follow := c.Query("follow") == "true"

	var limit int = 100
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = min(parsedLimit, 1000)
		}
	}

	opts := &GetLogsOptions{
		Level:  level,
		Stream: stream,
		Limit:  limit,
		Cursor: cursor,
		Follow: follow,
	}

	if startTime := c.Query("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			opts.StartTime = &t
		}
	}
	if endTime := c.Query("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			opts.EndTime = &t
		}
	}

	rawLogs, err := client.GetContainerLogsRaw(deploymentID, containerID, opts)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, rawLogs)
}

package transport

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/gin-gonic/gin"
)

func ReadTaskQuery(c *gin.Context) domain.TaskQuery {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	return domain.TaskQuery{
		Platform:       c.Query("platform"),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: start,
		EndTimestamp:   end,
		ChannelID:      c.Query("channel_id"),
	}
}
func ReadMidjourneyQuery(c *gin.Context) domain.MidjourneyQuery {
	return domain.MidjourneyQuery{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}
}

package controller

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
)

// BRIDGE: the system-task handler and immutable tick harness call the runtime owner.
func runMidjourneyTaskUpdateOnce(ctx context.Context, report func(processed, total int)) poll_midjourney_tasks.Summary {
	return taskPolling().RunMidjourney(ctx, report)
}

// BRIDGE: dashboard MJ router entrypoints; removed when router binds task handlers directly.
func GetAllMidjourney(c *gin.Context)  { taskHTTP().GetAllMidjourney(c) }
func GetUserMidjourney(c *gin.Context) { taskHTTP().GetUserMidjourney(c) }

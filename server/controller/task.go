package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/app/taskruntime"
)

// BRIDGE: existing router task entrypoints; removed when router binds the task module directly.
var taskHTTP = sync.OnceValue(taskruntime.NewHTTP)

func GetTask(c *gin.Context)                   { taskHTTP().GetTask(c) }
func GetTaskArtifacts(c *gin.Context)          { taskHTTP().GetTaskArtifacts(c) }
func GetDashboardTaskArtifacts(c *gin.Context) { taskHTTP().GetDashboardTaskArtifacts(c) }
func TaskArtifactContent(c *gin.Context)       { taskHTTP().TaskArtifactContent(c) }
func GetAllTask(c *gin.Context)                { taskHTTP().GetAllTask(c) }
func GetUserTask(c *gin.Context)               { taskHTTP().GetUserTask(c) }

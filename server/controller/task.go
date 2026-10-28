package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetTask(c *gin.Context) { httpapi.Tasks().GetTask(c) }

func GetTaskArtifacts(c *gin.Context) { httpapi.Tasks().GetTaskArtifacts(c) }

func GetDashboardTaskArtifacts(c *gin.Context) { httpapi.Tasks().GetDashboardTaskArtifacts(c) }

func TaskArtifactContent(c *gin.Context) { httpapi.Tasks().TaskArtifactContent(c) }

func GetAllTask(c *gin.Context) { httpapi.Tasks().GetAllTask(c) }

func GetUserTask(c *gin.Context) { httpapi.Tasks().GetUserTask(c) }

func ReleaseOpenRouterSyncHold(c *gin.Context) { httpapi.Tasks().ReleaseOpenRouterSyncHold(c) }
func SettleOpenRouterSyncCost(c *gin.Context)  { httpapi.Tasks().SettleOpenRouterSyncCost(c) }

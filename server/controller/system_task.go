package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func CreateLogCleanupSystemTask(c *gin.Context) { httpapi.System().StartCleanup.Handle(c) }

func GetCurrentSystemTask(c *gin.Context) { httpapi.System().CurrentTask.Handle(c) }

func ListSystemTasks(c *gin.Context) { httpapi.System().ListTasks.Handle(c) }

func GetSystemTask(c *gin.Context) { httpapi.System().GetTask.Handle(c) }

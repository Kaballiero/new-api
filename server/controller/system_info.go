package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func ListSystemInstances(c *gin.Context) { httpapi.System().Instances.Handle(c) }

func DeleteStaleSystemInstances(c *gin.Context) { httpapi.System().DeleteInstances.Handle(c) }

func DeleteStaleSystemInstance(c *gin.Context) { httpapi.System().DeleteInstance.Handle(c) }

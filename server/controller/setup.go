package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetSetup(c *gin.Context) { httpapi.System().GetSetup.Handle(c) }

func PostSetup(c *gin.Context) { httpapi.System().CompleteSetup.Handle(c) }

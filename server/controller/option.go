package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetOptions(c *gin.Context) { httpapi.Settings().GetOptions.Handle(c) }

func UpdateOption(c *gin.Context) { httpapi.Settings().UpdateOption.Handle(c) }

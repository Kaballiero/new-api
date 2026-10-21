package controller

import (
	"github.com/gin-gonic/gin"
)

func GetMissingModels(c *gin.Context) { catalogHTTP().ListMissingModels.Handle(c) }

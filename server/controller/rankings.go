package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetRankings(c *gin.Context) { httpapi.Usage().Rankings.Handle(c) }

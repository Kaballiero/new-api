package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GenerateOAuthCode(c *gin.Context) { httpapi.Identity().StartOAuthFlow.Handle(c) }

func HandleOAuth(c *gin.Context) { httpapi.Identity().CompleteOAuth.Handle(c) }

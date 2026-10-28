package controller

import (
	identitytransport "github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"
)

func TelegramLegacyAuth(c *gin.Context) { identitytransport.TelegramLegacyAuth(c) }

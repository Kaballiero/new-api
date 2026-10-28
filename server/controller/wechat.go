package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func WeChatAuth(c *gin.Context) { httpapi.Identity().LoginWeChat.Handle(c) }

func WeChatBind(c *gin.Context) { httpapi.Identity().BindWeChat.Handle(c) }

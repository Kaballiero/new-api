package controller

import "github.com/gin-gonic/gin"

func WeChatAuth(c *gin.Context) { identityHTTP().LoginWeChat.Handle(c) }
func WeChatBind(c *gin.Context) { identityHTTP().BindWeChat.Handle(c) }

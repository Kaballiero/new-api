package controller

import "github.com/gin-gonic/gin"

func WeChatAuth(c *gin.Context) { NewIdentityHTTP().LoginWeChat.Handle(c) }
func WeChatBind(c *gin.Context) { NewIdentityHTTP().BindWeChat.Handle(c) }

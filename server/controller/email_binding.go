package controller

import (
	"github.com/gin-gonic/gin"
)

func EmailBindStart(c *gin.Context)  { identityHTTP().StartEmailBinding.Handle(c) }
func EmailBindResend(c *gin.Context) { identityHTTP().ResendEmailBinding.Handle(c) }
func EmailBind(c *gin.Context)       { identityHTTP().ConfirmEmailBinding.Handle(c) }

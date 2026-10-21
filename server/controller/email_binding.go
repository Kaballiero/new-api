package controller

import (
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

type emailBindRequest = dto.EmailBindRequest

func EmailBindStart(c *gin.Context)  { identityHTTP().StartEmailBinding.Handle(c) }
func EmailBindResend(c *gin.Context) { identityHTTP().ResendEmailBinding.Handle(c) }
func EmailBind(c *gin.Context)       { identityHTTP().ConfirmEmailBinding.Handle(c) }

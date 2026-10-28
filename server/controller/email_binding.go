package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func EmailBindStart(c *gin.Context) { httpapi.Identity().StartEmailBinding.Handle(c) }

func EmailBindResend(c *gin.Context) { httpapi.Identity().ResendEmailBinding.Handle(c) }

func EmailBind(c *gin.Context) { httpapi.Identity().ConfirmEmailBinding.Handle(c) }

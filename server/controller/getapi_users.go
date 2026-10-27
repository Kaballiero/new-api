package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func ProvisionGetAPIUser(c *gin.Context)         { accountsHTTP().ProvisionAccount.Handle(c) }
func InitializeGetAPIPAT(c *gin.Context)         { accountsHTTP().InitializePAT.Handle(c) }
func writeGetAPIError(c *gin.Context, err error) { app.GetAPIErrors().Write(c, err) }

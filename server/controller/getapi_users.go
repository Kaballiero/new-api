package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func ProvisionGetAPIUser(c *gin.Context) { httpapi.Accounts().ProvisionAccount.Handle(c) }

func InitializeGetAPIPAT(c *gin.Context) { httpapi.Accounts().InitializePAT.Handle(c) }

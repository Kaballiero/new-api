package controller

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
)

func GetVerificationMethods(c *gin.Context) { NewIdentityHTTP().VerificationRequirements.Handle(c) }
func UniversalVerify(c *gin.Context)        { NewIdentityHTTP().VerifySecurityInput.Handle(c) }
func writeSecurityOperationError(c *gin.Context, err error) {
	identityhttp.WriteSecurityOperationError(c, err)
}

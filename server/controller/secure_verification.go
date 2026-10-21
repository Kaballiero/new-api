package controller

import (
	"github.com/gin-gonic/gin"
)

func GetVerificationMethods(c *gin.Context) { identityHTTP().VerificationRequirements.Handle(c) }
func UniversalVerify(c *gin.Context)        { identityHTTP().VerifySecurityInput.Handle(c) }
func writeSecurityOperationError(c *gin.Context, err error) {
	identityErrors.WriteError(c, err)
}

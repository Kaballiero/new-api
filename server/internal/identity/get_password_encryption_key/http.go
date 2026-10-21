package get_password_encryption_key

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ keys Keys }

func NewHTTP(keys Keys) *HTTP { return &HTTP{keys: keys} }
func (h *HTTP) Handle(c *gin.Context) {
	result, err := NewUsecase(h.keys).Execute()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	if !result.Enabled {
		common.ApiSuccess(c, gin.H{"enabled": false})
		return
	}
	common.ApiSuccess(c, gin.H{"enabled": true, "kid": result.KeyID, "public_key": result.PublicKey})
}

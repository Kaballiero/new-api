package get_key_usage

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, expiredAt, err := NewUsecase(h.reader, common.SysError).Execute(strings.TrimPrefix(tokenKey, "sk-"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": Usage{
			Object: "token_usage", Name: token.Name,
			TotalGranted: token.RemainQuota + token.UsedQuota, TotalUsed: token.UsedQuota,
			TotalAvailable: token.RemainQuota, UnlimitedQuota: token.UnlimitedQuota,
			ModelLimits: token.GetModelLimitsMap(), ModelLimitsEnabled: token.ModelLimitsEnabled,
			ExpiresAt: expiredAt,
		},
	})
}

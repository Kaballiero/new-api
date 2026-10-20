package get_key_status

import (
	tokenhttp "github.com/QuantumNous/new-api/internal/adapter/apikeyhttp"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, expiredAt, failure := NewUsecase(h.reader).Execute(tokenId, userId)
	if failure != nil {
		tokenhttp.WriteFailure(c, failure)
		return
	}
	c.JSON(http.StatusOK, CreditSummary{
		Object: "credit_summary", TotalGranted: token.RemainQuota,
		TotalUsed: 0, TotalAvailable: token.RemainQuota, ExpiresAt: expiredAt * 1000,
	})
}

package get_key_status

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/apikeys/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, expiredAt, err := NewUsecase(h.reader).Execute(tokenId, userId)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, CreditSummary{
		Object: "credit_summary", TotalGranted: token.RemainQuota,
		TotalUsed: 0, TotalAvailable: token.RemainQuota, ExpiresAt: expiredAt * 1000,
	})
}

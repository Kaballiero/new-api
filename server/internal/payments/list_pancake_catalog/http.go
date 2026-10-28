package list_pancake_catalog

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	result, err := h.usecase.Execute(transport.Context(c), Input{MerchantID: c.Query("merchant_id"), PrivateKey: c.Query("private_key")})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": dto.FromCatalog(result)})
}

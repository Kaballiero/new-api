package create_pancake_pair

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
)

type createWaffoPancakePairRequest struct {
	MerchantID string `json:"merchant_id"`
	PrivateKey string `json:"private_key"`
	ReturnURL  string `json:"return_url"`
}

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	var req createWaffoPancakePairRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
			return
		}
	}
	result, err := h.usecase.Execute(transport.Context(c), Input{MerchantID: req.MerchantID, PrivateKey: req.PrivateKey, ReturnURL: req.ReturnURL})
	if err != nil {
		if errors.Is(err, domain.ErrCredentials) {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
			return
		}
		data := gin.H{"error": err.Error()}
		if result != nil && result.OrphanStore {
			data["store_id"] = result.StoreID
			data["store_name"] = result.StoreName
			data["orphan_store"] = true
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": data})
		return
	}
	c.JSON(
		http.StatusOK,
		gin.H{
			"message": "success",
			"data":    gin.H{"store_id": result.StoreID, "store_name": result.StoreName, "product_id": result.ProductID, "product_name": result.ProductName},
		},
	)
}

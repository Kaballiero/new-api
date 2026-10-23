package save_pancake_config

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type saveWaffoPancakeRequest struct {
	MerchantID string `json:"merchant_id"`
	PrivateKey string `json:"private_key"`
	ReturnURL  string `json:"return_url"`
	StoreID    string `json:"store_id"`
	ProductID  string `json:"product_id"`
}

type HTTP struct {
	usecase     Usecase
	diagnostics domain.Diagnostics
}

func NewHTTP(usecase Usecase, diagnostics domain.Diagnostics) HTTP {
	return HTTP{usecase: usecase, diagnostics: diagnostics}
}

func (h HTTP) Handle(c *gin.Context) {
	var req saveWaffoPancakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	result, err := h.usecase.Execute(
		Input{MerchantID: req.MerchantID, PrivateKey: req.PrivateKey, ReturnURL: req.ReturnURL, StoreID: req.StoreID, ProductID: req.ProductID},
	)
	if err != nil {
		h.diagnostics.Error(transport.Context(c), "Waffo Pancake 保存配置失败 store_id=%q product_id=%q error=%q", req.StoreID, req.ProductID, err)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"store_id": result.StoreID, "product_id": result.ProductID}})
}

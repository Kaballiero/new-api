package list_pancake_products

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(u Usecase) HTTP { return HTTP{u} }
func (h HTTP) Handle(c *gin.Context) {
	result, err := h.usecase.Execute(c.Request.Context())
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	products := catalogProducts(result.Catalog, result.StoreID)
	c.JSON(200, gin.H{"message": "success", "data": gin.H{"store_id": result.StoreID, "products": products}})
}

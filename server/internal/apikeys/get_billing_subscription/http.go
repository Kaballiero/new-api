package get_billing_subscription

import (
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase    Usecase
	writeError func(*gin.Context, error)
}

func NewHTTP(usecase Usecase, writeError func(*gin.Context, error)) HTTP {
	return HTTP{usecase: usecase, writeError: writeError}
}
func (handler HTTP) Handle(c *gin.Context) {
	result, err := handler.usecase.Execute(domain.CompatibilityCaller{UserID: c.GetInt("id"), TokenID: c.GetInt("token_id")})
	if err != nil {
		handler.writeError(c, err)
		return
	}
	c.JSON(200, result)
}

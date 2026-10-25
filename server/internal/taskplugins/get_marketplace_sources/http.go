package get_marketplace_sources

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/gin-gonic/gin"
)

type Reader interface {
	Sources() []domain.MarketplaceSource
}
type Handler struct{ reader Reader }

func NewHandler(reader Reader) Handler     { return Handler{reader: reader} }
func (h Handler) ServeHTTP(c *gin.Context) { common.ApiSuccess(c, h.reader.Sources()) }

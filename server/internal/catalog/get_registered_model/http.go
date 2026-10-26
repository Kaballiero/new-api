package get_registered_model

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain/availability"
	"github.com/gin-gonic/gin"
)

type Reader interface {
	Registered(string) (availability.Model, bool)
}
type HTTP struct {
	reader Reader
	write  func(*gin.Context, int, string, *availability.Model)
}

func NewHTTP(reader Reader, write func(*gin.Context, int, string, *availability.Model)) HTTP {
	return HTTP{reader: reader, write: write}
}
func (h HTTP) Handle(c *gin.Context, format int) {
	name := c.Param("model")
	value, ok := h.reader.Registered(name)
	if !ok {
		h.write(c, format, name, nil)
		return
	}
	h.write(c, format, name, &value)
}

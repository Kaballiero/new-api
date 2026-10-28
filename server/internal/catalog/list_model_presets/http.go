package list_model_presets

import "github.com/gin-gonic/gin"

type Reader interface{ Presets() map[int][]string }
type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP     { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) { c.JSON(200, gin.H{"success": true, "data": h.reader.Presets()}) }

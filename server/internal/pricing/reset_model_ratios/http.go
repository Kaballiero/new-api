package reset_model_ratios

import (
	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP   { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) { c.JSON(200, h.usecase.Execute()) }

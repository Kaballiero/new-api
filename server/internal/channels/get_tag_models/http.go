package get_tag_models

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ store Store }

func NewHTTP(store Store) HTTP { return HTTP{store: store} }
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store)
	_ = scenario
	tag := c.Query("tag")
	if tag == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "tag不能为空"})
		return
	}
	models, err := scenario.Execute(Input{Tag: tag})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": models})
}

package get_permission_catalog

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	resources func() []ResourceDefinition
	roles     func() []RoleDescriptor
}

func NewHTTP(resources func() []ResourceDefinition, roles func() []RoleDescriptor) HTTP {
	return HTTP{resources: resources, roles: roles}
}
func (h HTTP) Handle(c *gin.Context) {
	output := NewUsecase(h.resources, h.roles).Execute()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": output})
}

package transport

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// RequestGroup returns the group of the authenticated user: the group set by
// the auth middleware, then the legacy "group" key, then a lookup by user id.
func RequestGroup(c *gin.Context, lookup func(int) (string, error)) (string, error) {
	if group := common.GetContextKeyString(c, constant.ContextKeyUserGroup); group != "" {
		return group, nil
	}
	if group := c.GetString("group"); group != "" {
		return group, nil
	}
	return lookup(c.GetInt("id"))
}

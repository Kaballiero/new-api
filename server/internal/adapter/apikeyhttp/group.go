package apikeyhttp

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

func RequestGroup(c *gin.Context, lookup func(int) (string, error)) (string, error) {
	if group := common.GetContextKeyString(c, constant.ContextKeyUserGroup); group != "" {
		return group, nil
	}
	if group := c.GetString("group"); group != "" {
		return group, nil
	}
	return lookup(c.GetInt("id"))
}

type GroupSource struct {
	Read  func() (string, error)
	List  func(string) []string
	Limit func() int
}

func (s GroupSource) Group() (string, error)        { return s.Read() }
func (s GroupSource) Options(group string) []string { return s.List(group) }
func (s GroupSource) Max() int                      { return s.Limit() }

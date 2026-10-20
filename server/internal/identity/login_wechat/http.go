package login_wechat

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Completion interface {
	Operation
	Enabled() bool
	AuthenticatedUser() *model.User
}
type HTTP struct {
	factory  func() Completion
	complete func(*model.User, *gin.Context)
}

func NewHTTP(factory func() Completion, complete func(*model.User, *gin.Context)) *HTTP {
	return &HTTP{factory: factory, complete: complete}
}
func (h *HTTP) Handle(c *gin.Context) {
	op := h.factory()
	if !op.Enabled() {
		c.JSON(http.StatusOK, gin.H{"message": "管理员未开启通过微信登录以及注册", "success": false})
		return
	}
	result := NewUsecase(op).Execute(Input{Code: c.Query("code")})
	message := ""
	if result.Cause != nil {
		message = result.Cause.Error()
	} else {
		switch result.Issue {
		case "disabled":
			message = "管理员未开启通过微信登录以及注册"
		case "deleted":
			message = "用户已注销"
		case "registration_disabled":
			message = "管理员关闭了新用户注册"
		case "banned":
			message = "用户已被封禁"
		}
	}
	if result.Cause != nil || result.Issue != "" {
		c.JSON(http.StatusOK, gin.H{"message": message, "success": false})
		return
	}
	h.complete(op.AuthenticatedUser(), c)
}

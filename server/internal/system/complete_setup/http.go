package complete_setup

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/gin-gonic/gin"
)

type Setup interface{ Execute(Input) error }

type Handler struct {
	usecase Setup
	setup   func() bool
	root    func() bool
}

func NewHandler(usecase Setup, setup, root func() bool) Handler {
	return Handler{usecase: usecase, setup: setup, root: root}
}
func (h Handler) Handle(c *gin.Context) {
	if h.setup() {
		c.JSON(200, gin.H{"success": false, "message": "系统已经初始化完成"})
		return
	}
	rootExists := h.root()
	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"success": false, "message": "请求参数有误"})
		return
	}
	err := h.usecase.Execute(Input{
		Username:           req.Username,
		Password:           req.Password,
		ConfirmPassword:    req.ConfirmPassword,
		SelfUseModeEnabled: req.SelfUseModeEnabled,
		DemoSiteEnabled:    req.DemoSiteEnabled,
		RootExists:         rootExists,
	})
	if err != nil {
		cause := err
		var wrapped interface{ Unwrap() []error }
		if errors.As(err, &wrapped) {
			if causes := wrapped.Unwrap(); len(causes) == 2 && causes[1] != nil {
				cause = causes[1]
			}
		}
		message := "系统错误: " + cause.Error()
		switch {
		case errors.Is(err, domain.ErrSetupUsername):
			message = "用户名长度不能超过12个字符"
		case errors.Is(err, domain.ErrSetupPasswordMismatch):
			message = "两次输入的密码不一致"
		case errors.Is(err, domain.ErrSetupPassword):
			message = cause.Error()
		case errors.Is(err, domain.ErrSetupHash):
			message = "系统错误: " + cause.Error()
		case errors.Is(err, domain.ErrSetupRoot):
			message = "创建管理员账号失败: " + cause.Error()
		case errors.Is(err, domain.ErrSetupSelfMode):
			message = "保存自用模式设置失败: " + cause.Error()
		case errors.Is(err, domain.ErrSetupDemoMode):
			message = "保存演示站点模式设置失败: " + cause.Error()
		case errors.Is(err, domain.ErrSetupPersist):
			message = "系统初始化失败: " + cause.Error()
		}
		c.JSON(200, gin.H{"success": false, "message": message})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "系统初始化成功"})
}

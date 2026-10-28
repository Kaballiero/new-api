package complete_setup

import (
	"errors"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/transport"

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
		c.JSON(200, gin.H{"success": false, "message": transport.Message(c, i18n.MsgSystemSetupComplete)})
		return
	}
	rootExists := h.root()
	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"success": false, "message": transport.Message(c, i18n.MsgSystemInvalidSetup)})
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
		message := transport.Message(c, i18n.MsgSystemSystemError, map[string]any{"Cause": cause.Error()})
		switch {
		case errors.Is(err, domain.ErrSetupUsername):
			message = transport.Message(c, i18n.MsgSystemUsernameLength)
		case errors.Is(err, domain.ErrSetupPasswordMismatch):
			message = transport.Message(c, i18n.MsgSystemPasswordMismatch)
		case errors.Is(err, domain.ErrSetupPassword):
			message = cause.Error()
		case errors.Is(err, domain.ErrSetupHash):
			message = transport.Message(c, i18n.MsgSystemSystemError, map[string]any{"Cause": cause.Error()})
		case errors.Is(err, domain.ErrSetupRoot):
			message = transport.Message(c, i18n.MsgSystemCreateRoot, map[string]any{"Cause": cause.Error()})
		case errors.Is(err, domain.ErrSetupSelfMode):
			message = transport.Message(c, i18n.MsgSystemSaveSelfMode, map[string]any{"Cause": cause.Error()})
		case errors.Is(err, domain.ErrSetupDemoMode):
			message = transport.Message(c, i18n.MsgSystemSaveDemoMode, map[string]any{"Cause": cause.Error()})
		case errors.Is(err, domain.ErrSetupPersist):
			message = transport.Message(c, i18n.MsgSystemInitialize, map[string]any{"Cause": cause.Error()})
		}
		c.JSON(200, gin.H{"success": false, "message": message})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": transport.Message(c, i18n.MsgSystemInitialized)})
}

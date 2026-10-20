package controller

import (
	"encoding/json"
	"errors"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type passkeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
}

type passkeyVerifyBeginRequest struct {
	Scope   string          `json:"scope"`
	Context json.RawMessage `json:"context,omitempty"`
}

func parsePasskeyFinishRequest(c *gin.Context) (*passkeyFinishRequest, error) {
	var request passkeyFinishRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		return nil, err
	}
	if request.FlowToken == "" || len(request.Credential) == 0 {
		return nil, errors.New("Passkey 流程参数不完整")
	}
	return &request, nil
}

func PasskeyRegisterBegin(c *gin.Context) { NewIdentityHTTP().BeginPasskeyRegistration.Handle(c) }

func PasskeyRegisterFinish(c *gin.Context) { NewIdentityHTTP().FinishPasskeyRegistration.Handle(c) }

func PasskeyDelete(c *gin.Context) { NewIdentityHTTP().DeletePasskey.Handle(c) }

func PasskeyStatus(c *gin.Context) { NewIdentityHTTP().GetPasskeyStatus.Handle(c) }

func PasskeyLoginBegin(c *gin.Context) { NewIdentityHTTP().BeginPasskeyLogin.Handle(c) }

func PasskeyLoginFinish(c *gin.Context) { NewIdentityHTTP().FinishPasskeyLogin.Handle(c) }

func AdminResetPasskey(c *gin.Context) { NewIdentityHTTP().AdminResetPasskey.Handle(c) }

func PasskeyVerifyBegin(c *gin.Context) { NewIdentityHTTP().BeginPasskeyVerification.Handle(c) }

func PasskeyVerifyFinish(c *gin.Context) { NewIdentityHTTP().FinishPasskeyVerification.Handle(c) }

func getAuthenticatedUser(c *gin.Context) (*model.User, error) {
	return identityhttp.GetAuthenticatedUser(c)
}

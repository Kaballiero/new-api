package controller

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type passkeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
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

func PasskeyRegisterBegin(c *gin.Context) { identityHTTP().BeginPasskeyRegistration.Handle(c) }

func PasskeyRegisterFinish(c *gin.Context) { identityHTTP().FinishPasskeyRegistration.Handle(c) }

func PasskeyDelete(c *gin.Context) { identityHTTP().DeletePasskey.Handle(c) }

func PasskeyStatus(c *gin.Context) { identityHTTP().GetPasskeyStatus.Handle(c) }

func PasskeyLoginBegin(c *gin.Context) { identityHTTP().BeginPasskeyLogin.Handle(c) }

func PasskeyLoginFinish(c *gin.Context) { identityHTTP().FinishPasskeyLogin.Handle(c) }

func AdminResetPasskey(c *gin.Context) { identityHTTP().AdminResetPasskey.Handle(c) }

func PasskeyVerifyBegin(c *gin.Context) { identityHTTP().BeginPasskeyVerification.Handle(c) }

func PasskeyVerifyFinish(c *gin.Context) { identityHTTP().FinishPasskeyVerification.Handle(c) }

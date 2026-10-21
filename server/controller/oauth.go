package controller

import (
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauth"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_flow"
	"github.com/gin-gonic/gin"
)

type oauthStateRequest = start_oauth_flow.Request
type oauthFlowPayload = oauthadapter.FlowPayload

func GenerateOAuthCode(c *gin.Context) { identityHTTP().StartOAuthFlow.Handle(c) }
func HandleOAuth(c *gin.Context)       { identityHTTP().CompleteOAuth.Handle(c) }

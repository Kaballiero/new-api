package controller

import (
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauth"
	"github.com/QuantumNous/new-api/internal/identity/complete_oauth"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_flow"
	"github.com/gin-gonic/gin"
)

type oauthStateRequest = start_oauth_flow.Request
type oauthFlowPayload = oauthadapter.FlowPayload
type OAuthUserDeletedError = complete_oauth.OAuthUserDeletedError
type OAuthRegistrationDisabledError = complete_oauth.OAuthRegistrationDisabledError
type OAuthEmailAlreadyTakenError = complete_oauth.OAuthEmailAlreadyTakenError

func GenerateOAuthCode(c *gin.Context) { NewIdentityHTTP().StartOAuthFlow.Handle(c) }
func HandleOAuth(c *gin.Context)       { NewIdentityHTTP().CompleteOAuth.Handle(c) }

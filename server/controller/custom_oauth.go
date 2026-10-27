package controller

import (
	"github.com/QuantumNous/new-api/internal/identity/get_oauth_bindings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/app"
	"sync"
)

// BRIDGE: router callers retain their existing custom OAuth handler names.
var customOAuthHTTP = sync.OnceValue(app.NewCustomOAuthHTTP)

type UserOAuthBindingResponse = get_oauth_bindings.UserOAuthBindingResponse

func GetCustomOAuthProviders(c *gin.Context)   { customOAuthHTTP().List.Handle(c) }
func GetCustomOAuthProvider(c *gin.Context)    { customOAuthHTTP().Get.Handle(c) }
func CreateCustomOAuthProvider(c *gin.Context) { customOAuthHTTP().Create.Handle(c) }
func UpdateCustomOAuthProvider(c *gin.Context) { customOAuthHTTP().Update.Handle(c) }
func DeleteCustomOAuthProvider(c *gin.Context) { customOAuthHTTP().Delete.Handle(c) }
func FetchCustomOAuthDiscovery(c *gin.Context) { customOAuthHTTP().Discovery.Handle(c) }

// GetUserOAuthBindings returns all OAuth bindings for the current user
func GetUserOAuthBindings(c *gin.Context) { identityHTTP().GetOAuthBindings.Self(c) }

func GetUserOAuthBindingsByAdmin(c *gin.Context) { identityHTTP().GetOAuthBindings.Admin(c) }

// UnbindCustomOAuth unbinds a custom OAuth provider from the current user
func UnbindCustomOAuth(c *gin.Context) { identityHTTP().UnlinkOAuthAccount.Handle(c) }

func UnbindCustomOAuthByAdmin(c *gin.Context) { identityHTTP().AdminUnlinkOAuthAccount.Handle(c) }

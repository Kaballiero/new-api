package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/identity/get_oauth_bindings"
	"github.com/gin-gonic/gin"
)

type UserOAuthBindingResponse = get_oauth_bindings.UserOAuthBindingResponse

func GetCustomOAuthProviders(c *gin.Context) { httpapi.CustomOAuth().List.Handle(c) }

func GetCustomOAuthProvider(c *gin.Context) { httpapi.CustomOAuth().Get.Handle(c) }

func CreateCustomOAuthProvider(c *gin.Context) { httpapi.CustomOAuth().Create.Handle(c) }

func UpdateCustomOAuthProvider(c *gin.Context) { httpapi.CustomOAuth().Update.Handle(c) }

func DeleteCustomOAuthProvider(c *gin.Context) { httpapi.CustomOAuth().Delete.Handle(c) }

func FetchCustomOAuthDiscovery(c *gin.Context) { httpapi.CustomOAuth().Discovery.Handle(c) }

func GetUserOAuthBindings(c *gin.Context) { httpapi.Identity().GetOAuthBindings.Self(c) }

func GetUserOAuthBindingsByAdmin(c *gin.Context) { httpapi.Identity().GetOAuthBindings.Admin(c) }

func UnbindCustomOAuth(c *gin.Context) { httpapi.Identity().UnlinkOAuthAccount.Handle(c) }

func UnbindCustomOAuthByAdmin(c *gin.Context) { httpapi.Identity().AdminUnlinkOAuthAccount.Handle(c) }

package httpapi

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	paymentsapp "github.com/QuantumNous/new-api/internal/app/payments"
	redemptionapp "github.com/QuantumNous/new-api/internal/app/redemption"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	subscriptionsapp "github.com/QuantumNous/new-api/internal/app/subscriptions"
	"github.com/QuantumNous/new-api/internal/app/systemruntime"
	pluginapp "github.com/QuantumNous/new-api/internal/app/taskplugins"
	"github.com/QuantumNous/new-api/internal/app/taskruntime"
	appusage "github.com/QuantumNous/new-api/internal/app/usage"
	audittransport "github.com/QuantumNous/new-api/internal/audit/transport"

	"github.com/gin-gonic/gin"
)

// PersonalAccessTokens builds the personal access token handlers once; the
// database is read per request by the injected store.
var PersonalAccessTokens = sync.OnceValue(func() app.PersonalAccessTokensHTTP {
	return app.NewPersonalAccessTokensHTTP(recordUserSecurityAudit)
})

var Audit = sync.OnceValue(auditapp.NewHTTP)

// Authorization builds the permission catalog handler once; the catalog is
// read per request by the injected functions.
var Authorization = sync.OnceValue(app.NewAuthorizationHTTP)

var BillingCompatibility = sync.OnceValue(app.NewBillingCompatibilityHTTP)

var CustomOAuth = sync.OnceValue(app.NewCustomOAuthHTTP)

var Settings = sync.OnceValue(settingsapp.NewHTTP)

var Redemption = sync.OnceValue(redemptionapp.NewHTTP)

var Subscriptions = sync.OnceValue(subscriptionsapp.NewHTTP)

var SubscriptionProviders = sync.OnceValue(subscriptionsapp.NewProviders)

var System = sync.OnceValue(systemruntime.NewHTTP)

var Tasks = sync.OnceValue(taskruntime.NewHTTP)

var UploadTaskPlugin = sync.OnceValue(pluginapp.NewUploadTaskPlugin)

var ListTaskPlugins = sync.OnceValue(pluginapp.NewListTaskPlugins)

var GetTaskPluginRuntime = sync.OnceValue(pluginapp.NewGetTaskPluginRuntime)

var GetTaskPlugin = sync.OnceValue(pluginapp.NewGetTaskPlugin)

var ActivateTaskPlugin = sync.OnceValue(pluginapp.NewActivateTaskPlugin)

var DeleteTaskPluginVersion = sync.OnceValue(pluginapp.NewDeleteTaskPluginVersion)

var SetTaskPluginStatus = sync.OnceValue(pluginapp.NewSetTaskPluginStatus)

var UpdateTaskPluginMarketplaceSources = sync.OnceValue(pluginapp.NewUpdateTaskPluginMarketplaceSources)

var GetTaskPluginOptions = sync.OnceValue(pluginapp.NewGetTaskPluginOptions)

var GetTaskPluginVersions = sync.OnceValue(pluginapp.NewGetTaskPluginVersions)

var GetTaskPluginMarketplaceSources = sync.OnceValue(pluginapp.NewGetTaskPluginMarketplaceSources)

var GetTaskPluginIcon = sync.OnceValue(pluginapp.NewGetTaskPluginIcon)

var DryRunTaskPlugin = sync.OnceValue(pluginapp.NewDryRunTaskPlugin)

// APIKeys builds the API key handlers once; settings and the database are
// read per request by the injected functions.
var APIKeys = sync.OnceValue(func() app.APIKeysHTTP {
	return app.NewAPIKeysHTTP(app.APIKeyEffects{Audit: func(c *gin.Context) map[string]any { return audittransport.TokenParameters(c) }, BatchAudit: func(c *gin.Context, ids []int) map[string]any { return audittransport.BatchParameters(c, ids) }})
})

var Payments = sync.OnceValue(paymentsapp.NewHTTP)

var Usage = sync.OnceValue(appusage.NewHTTP)

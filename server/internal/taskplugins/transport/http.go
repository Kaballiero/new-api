package transport

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/i18n"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/gin-gonic/gin"
)

func Context(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}
func Error(c *gin.Context, err error) {
	var used *domain.InUseError
	if errors.As(err, &used) {
		c.JSON(200, gin.H{"success": false, "message": i18n.Translate(Language(c), i18n.MsgTaskPluginInUse), "data": gin.H{"channels": used.Channels, "in_flight_count": used.InFlight}})
		return
	}
	key := ""
	switch {
	case errors.Is(err, domain.ErrSourceTooLarge):
		key = i18n.MsgTaskPluginSourceTooLarge
	case errors.Is(err, domain.ErrSourceHash):
		key = i18n.MsgTaskPluginSourceHash
	case errors.Is(err, domain.ErrPluginNotFound):
		key = i18n.MsgTaskPluginPluginNotFound
	case errors.Is(err, domain.ErrVersionNotFound):
		key = i18n.MsgTaskPluginVersionNotFound
	case errors.Is(err, domain.ErrDeleteFactory):
		key = i18n.MsgTaskPluginDeleteFactory
	case errors.Is(err, domain.ErrMarketplaceName):
		key = i18n.MsgTaskPluginMarketplaceName
	case errors.Is(err, domain.ErrMarketplaceURL):
		key = i18n.MsgTaskPluginMarketplaceURL
	case errors.Is(err, domain.ErrSourceConflict):
		key = i18n.MsgTaskPluginSourceConflict
	}
	if key != "" {
		common.ApiErrorMsg(c, i18n.Translate(Language(c), key))
		return
	}
	common.ApiError(c, err)
}

// Language is safe for direct transport calls without an HTTP request.
func Language(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return i18n.DefaultLang
	}
	return i18n.GetLangFromContext(c)
}

package refresh_codex_credential

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	oauthKey, ch, err := h.usecase(c).Execute(ctx, Input{ChannelID: channelId, ResetCaches: true})
	if err != nil {
		common.SysError("failed to refresh codex channel credential: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgChannelCodexRefreshFailed)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "refreshed",
		"data": gin.H{
			"expires_at":   oauthKey.Expired,
			"last_refresh": oauthKey.LastRefresh,
			"account_id":   oauthKey.AccountID,
			"email":        oauthKey.Email,
			"channel_id":   ch.Id,
			"channel_type": ch.Type,
			"channel_name": ch.Name,
		},
	})
}

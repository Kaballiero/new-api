package controller

import (
	"github.com/QuantumNous/new-api/common"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

type OpenAISubscriptionResponse = channeladapter.OpenAISubscriptionResponse
type OpenAIUsageDailyCost = channeladapter.OpenAIUsageDailyCost
type OpenAICreditGrants = channeladapter.OpenAICreditGrants
type OpenAIUsageResponse = channeladapter.OpenAIUsageResponse
type OpenAISBUsageResponse = channeladapter.OpenAISBUsageResponse
type AIProxyUserOverviewResponse = channeladapter.AIProxyUserOverviewResponse
type API2GPTUsageResponse = channeladapter.API2GPTUsageResponse
type APGC2DGPTUsageResponse = channeladapter.APGC2DGPTUsageResponse
type SiliconFlowUsageResponse = channeladapter.SiliconFlowUsageResponse
type DeepSeekUsageResponse = channeladapter.DeepSeekUsageResponse
type OpenRouterCreditResponse = channeladapter.OpenRouterCreditResponse

func GetAuthHeader(token string) http.Header       { return channeladapter.GetAuthHeader(token) }
func GetClaudeAuthHeader(token string) http.Header { return channeladapter.GetClaudeAuthHeader(token) }
func GetResponseBody(method, url string, channel *model.Channel, headers http.Header) ([]byte, error) {
	return channeladapter.GetResponseBody(method, url, channel, headers)
}
func deepSeekBalanceUSD(response DeepSeekUsageResponse) (float64, error) {
	return channeladapter.DeepSeekBalanceUSD(response)
}
func updateChannelCloseAIBalance(channel *model.Channel) (float64, error) {
	balance, err := channeladapter.UpdateChannelCloseAIBalance(channel)
	if err == nil {
		channel.UpdateBalance(balance)
	}
	return balance, err
}
func updateChannelOpenAISBBalance(channel *model.Channel) (float64, error) {
	balance, err := channeladapter.UpdateChannelOpenAISBBalance(channel)
	if err == nil {
		channel.UpdateBalance(balance)
	}
	return balance, err
}
func UpdateChannelBalance(c *gin.Context) {
	app.NewUpdateChannelBalanceHTTP(c, channelEffects()).Handle(c)
}
func UpdateAllChannelsBalance(c *gin.Context) {
	app.NewUpdateAllChannelsBalanceHTTP(c, channelEffects()).Handle(c)
}
func updateAllChannelsBalance() error { return app.NewChannelBalancesUsecase().Execute() }
func AutomaticallyUpdateChannels(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		common.SysLog("updating all channels")
		_ = updateAllChannelsBalance()
		common.SysLog("channels update done")
	}
}

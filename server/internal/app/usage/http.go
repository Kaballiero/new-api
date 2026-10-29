package usage

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	memory "github.com/QuantumNous/new-api/internal/adapter/memory/usage"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/usage/admin_get_log_stats"
	"github.com/QuantumNous/new-api/internal/usage/admin_get_usage_flow"
	"github.com/QuantumNous/new-api/internal/usage/admin_list_logs"
	"github.com/QuantumNous/new-api/internal/usage/admin_list_usage_data"
	"github.com/QuantumNous/new-api/internal/usage/admin_list_user_usage_data"
	"github.com/QuantumNous/new-api/internal/usage/admin_query_user_logs"
	"github.com/QuantumNous/new-api/internal/usage/aggregate_logs"
	"github.com/QuantumNous/new-api/internal/usage/aggregate_usage_data"
	"github.com/QuantumNous/new-api/internal/usage/get_own_log_stats"
	"github.com/QuantumNous/new-api/internal/usage/get_own_usage_flow"
	"github.com/QuantumNous/new-api/internal/usage/get_rankings"
	"github.com/QuantumNous/new-api/internal/usage/getapi_aggregate"
	"github.com/QuantumNous/new-api/internal/usage/getapi_logs"
	"github.com/QuantumNous/new-api/internal/usage/list_key_logs"
	"github.com/QuantumNous/new-api/internal/usage/list_own_logs"
	"github.com/QuantumNous/new-api/internal/usage/list_own_usage_data"
)

type HTTP struct {
	GetAPIAggregate getapi_aggregate.Handler
	GetAPILogs      getapi_logs.Handler
	LogAggregate    aggregate_logs.Handler
	UsageAggregate  aggregate_usage_data.Handler

	AdminQuota admin_list_usage_data.Handler
	UserQuota  admin_list_user_usage_data.Handler
	OwnQuota   list_own_usage_data.Handler
	AdminFlow  admin_get_usage_flow.Handler
	OwnFlow    get_own_usage_flow.Handler

	Rankings      get_rankings.Handler
	AdminLogs     admin_list_logs.Handler
	OwnLogs       list_own_logs.Handler
	KeyLogs       list_key_logs.Handler
	AdminStats    admin_get_log_stats.Handler
	OwnStats      get_own_log_stats.Handler
	QueryUserLogs admin_query_user_logs.Handler
}

func NewHTTP() HTTP {
	store := usagestorage.Store()
	store.RankingCatalog = func() ([]pricingdomain.PricingVendor, []pricingdomain.Pricing) {
		vendors := pricingruntime.GetVendors()
		prices := pricingruntime.GetPricing()
		return vendors, prices
	}
	return HTTP{
		GetAPIAggregate: getapi_aggregate.NewHandler(getapi_aggregate.NewUsecase(store, func() int64 { return time.Now().Unix() })),
		GetAPILogs:      getapi_logs.NewHandler(getapi_logs.NewUsecase(store)),
		LogAggregate:    aggregate_logs.NewHandler(aggregate_logs.NewUsecase(store)),
		UsageAggregate:  aggregate_usage_data.NewHandler(aggregate_usage_data.NewUsecase(store)),
		AdminQuota:      admin_list_usage_data.NewHandler(admin_list_usage_data.NewUsecase(store)),
		UserQuota:       admin_list_user_usage_data.NewHandler(admin_list_user_usage_data.NewUsecase(store)),
		OwnQuota:        list_own_usage_data.NewHandler(list_own_usage_data.NewUsecase(store)),
		AdminFlow:       admin_get_usage_flow.NewHandler(admin_get_usage_flow.NewUsecase(store)),
		OwnFlow:         get_own_usage_flow.NewHandler(get_own_usage_flow.NewUsecase(store)),

		AdminLogs:     admin_list_logs.NewHandler(admin_list_logs.NewUsecase(store)),
		OwnLogs:       list_own_logs.NewHandler(list_own_logs.NewUsecase(store)),
		KeyLogs:       list_key_logs.NewHandler(list_key_logs.NewUsecase(store, func() int { return common.MaxRecentItems })),
		AdminStats:    admin_get_log_stats.NewHandler(admin_get_log_stats.NewUsecase(store, time.Now)),
		OwnStats:      get_own_log_stats.NewHandler(get_own_log_stats.NewUsecase(store, time.Now)),
		QueryUserLogs: admin_query_user_logs.NewHandler(admin_query_user_logs.NewUsecase(store)),
		Rankings:      get_rankings.NewHandler(get_rankings.NewUsecase(store, store, memory.NewRankingsCache(), time.Now))}
}

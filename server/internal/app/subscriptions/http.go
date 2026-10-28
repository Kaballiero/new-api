package subscriptions

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/app/dbclock"
	"github.com/QuantumNous/new-api/internal/app/subscriptionstorage"
	paymentsdomain "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/admin_list_plans"
	"github.com/QuantumNous/new-api/internal/subscriptions/admin_list_user_subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/bind_subscription"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_plan"
	"github.com/QuantumNous/new-api/internal/subscriptions/delete_subscription"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/get_own_subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/invalidate_subscription"
	"github.com/QuantumNous/new-api/internal/subscriptions/list_plans"
	"github.com/QuantumNous/new-api/internal/subscriptions/purchase_with_balance"
	"github.com/QuantumNous/new-api/internal/subscriptions/reset_subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/update_billing_preference"
	"github.com/QuantumNous/new-api/internal/subscriptions/update_plan"
	"github.com/QuantumNous/new-api/internal/subscriptions/update_plan_status"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type HTTP struct {
	Bind       bind_subscription.HTTP
	Reset      reset_subscriptions.HTTP
	Invalidate invalidate_subscription.HTTP
	Delete     delete_subscription.HTTP
	Balance    purchase_with_balance.HTTP

	CreatePlan create_plan.HTTP
	UpdatePlan update_plan.HTTP
	PlanStatus update_plan_status.HTTP

	ListPlans         list_plans.HTTP
	ListAllPlans      admin_list_plans.HTTP
	Own               get_own_subscriptions.HTTP
	Preference        update_billing_preference.HTTP
	UserSubscriptions admin_list_user_subscriptions.HTTP
}

func NewHTTP() HTTP {
	store := subscriptionstorage.SQL()
	rules := domain.PlanRules{GroupExists: func(group string) bool { _, ok := ratio_setting.GetGroupRatioCopy()[group]; return ok }}
	return HTTP{
		Bind: bind_subscription.NewHTTP(
			bind_subscription.NewUsecase(store, common.GetTimestamp, effects{}.RefreshGroup),
			operation_setting.IsPaymentComplianceConfirmed,
		),
		Reset:      reset_subscriptions.NewHTTP(reset_subscriptions.NewUsecase(store, dbclock.Now), NewResetAudit()),
		Invalidate: invalidate_subscription.NewHTTP(invalidate_subscription.NewUsecase(store, common.GetTimestamp, effects{}.RefreshGroup)),
		Delete:     delete_subscription.NewHTTP(delete_subscription.NewUsecase(store, common.GetTimestamp, effects{}.RefreshGroup)),
		Balance: purchase_with_balance.NewHTTP(
			purchase_with_balance.NewUsecase(store, effects{}, quotaMath{}, common.GetTimestamp, func(userID int) string {
				return fmt.Sprintf("SUBBALUSR%dNO%s%d", userID, common.GetRandomString(6), time.Now().UnixNano())
			}, paymentsdomain.PaymentMethodBalance, paymentsdomain.PaymentProviderBalance, paymentsdomain.StatusSuccess),
			operation_setting.IsPaymentComplianceConfirmed,
		),
		CreatePlan: create_plan.NewHTTP(
			create_plan.NewUsecase(store, rules, subscriptionstorage.Cache().InvalidateSubscriptionPlanCache),
			operation_setting.IsPaymentComplianceConfirmed,
		),
		UpdatePlan: update_plan.NewHTTP(
			update_plan.NewUsecase(store, rules, subscriptionstorage.Cache().InvalidateSubscriptionPlanCache),
			operation_setting.IsPaymentComplianceConfirmed,
		),
		PlanStatus: update_plan_status.NewHTTP(
			update_plan_status.NewUsecase(store, subscriptionstorage.Cache().InvalidateSubscriptionPlanCache),
			operation_setting.IsPaymentComplianceConfirmed,
		),
		ListPlans:         list_plans.NewHTTP(list_plans.NewUsecase(store, operation_setting.IsPaymentComplianceConfirmed)),
		ListAllPlans:      admin_list_plans.NewHTTP(admin_list_plans.NewUsecase(store)),
		Own:               get_own_subscriptions.NewHTTP(get_own_subscriptions.NewUsecase(store, common.NormalizeBillingPreference, common.GetTimestamp)),
		Preference:        update_billing_preference.NewHTTP(update_billing_preference.NewUsecase(store, common.NormalizeBillingPreference)),
		UserSubscriptions: admin_list_user_subscriptions.NewHTTP(admin_list_user_subscriptions.NewUsecase(store)),
	}
}

package subscriptions

import (
	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/admin_list_plans"
	"github.com/QuantumNous/new-api/internal/subscriptions/admin_list_user_subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_plan"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/get_own_subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/list_plans"
	"github.com/QuantumNous/new-api/internal/subscriptions/update_billing_preference"
	"github.com/QuantumNous/new-api/internal/subscriptions/update_plan"
	"github.com/QuantumNous/new-api/internal/subscriptions/update_plan_status"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

type HTTP struct {
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
	store := storage.NewStore(func() *gorm.DB { return model.DB })
	rules := domain.PlanRules{GroupExists: func(group string) bool { _, ok := ratio_setting.GetGroupRatioCopy()[group]; return ok }, NormalizeResetPeriod: model.NormalizeResetPeriod}
	return HTTP{
		CreatePlan:        create_plan.NewHTTP(create_plan.NewUsecase(store, rules, model.InvalidateSubscriptionPlanCache), operation_setting.IsPaymentComplianceConfirmed),
		UpdatePlan:        update_plan.NewHTTP(update_plan.NewUsecase(store, rules, model.InvalidateSubscriptionPlanCache), operation_setting.IsPaymentComplianceConfirmed),
		PlanStatus:        update_plan_status.NewHTTP(update_plan_status.NewUsecase(store, model.InvalidateSubscriptionPlanCache), operation_setting.IsPaymentComplianceConfirmed),
		ListPlans:         list_plans.NewHTTP(list_plans.NewUsecase(store, operation_setting.IsPaymentComplianceConfirmed)),
		ListAllPlans:      admin_list_plans.NewHTTP(admin_list_plans.NewUsecase(store)),
		Own:               get_own_subscriptions.NewHTTP(get_own_subscriptions.NewUsecase(store, common.NormalizeBillingPreference, common.GetTimestamp)),
		Preference:        update_billing_preference.NewHTTP(update_billing_preference.NewUsecase(store, common.NormalizeBillingPreference)),
		UserSubscriptions: admin_list_user_subscriptions.NewHTTP(admin_list_user_subscriptions.NewUsecase(store)),
	}
}

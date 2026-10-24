package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// Resolve stable router entrypoints to subscriptions transports, preserving published wrapper schemas.
func parseSubscriptionsHandlers() error {
	for _, source := range []struct{ handler, scenario, method string }{
		{"GetSubscriptionPlans", "list_plans", "Handle"},
		{"GetSubscriptionSelf", "get_own_subscriptions", "Handle"},
		{"UpdateSubscriptionPreference", "update_billing_preference", "Handle"},
		{"SubscriptionRequestBalancePay", "purchase_with_balance", "Handle"},
		{"AdminListSubscriptionPlans", "admin_list_plans", "Handle"},
		{"AdminCreateSubscriptionPlan", "create_plan", "Handle"},
		{"AdminUpdateSubscriptionPlan", "update_plan", "Handle"},
		{"AdminUpdateSubscriptionPlanStatus", "update_plan_status", "Handle"},
		{"AdminBindSubscription", "bind_subscription", "Handle"},
		{"AdminCreateUserSubscription", "bind_subscription", "CreateForUser"},
		{"AdminListUserSubscriptions", "admin_list_user_subscriptions", "Handle"},
		{"AdminResetUserSubscriptionsByPlan", "reset_subscriptions", "handle"},
		{"AdminResetPlanSubscriptions", "reset_subscriptions", "handle"},
		{"AdminInvalidateUserSubscription", "invalidate_subscription", "Handle"},
		{"AdminDeleteUserSubscription", "delete_subscription", "Handle"},
		{"SubscriptionRequestStripePay", "create_stripe_order", "Handle"},
		{"SubscriptionRequestCreemPay", "create_creem_order", "Handle"},
		{"SubscriptionRequestEpay", "create_epay_order", "Handle"},
		{"SubscriptionEpayNotify", "receive_epay_callback", "Notify"},
		{"SubscriptionEpayReturn", "receive_epay_callback", "Return"},
		{"SubscriptionRequestWaffoPancakePay", "create_pancake_order", "Handle"},
		{"CreateWaffoPancakeSubscriptionProduct", "create_pancake_product", "Handle"},
		{"ListWaffoPancakeSubscriptionProductOptions", "list_pancake_products", "Handle"},
	} {
		dir := filepath.Join("internal/subscriptions", source.scenario)
		if err := parseModels(dir); err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "http.go"), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		found := false
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != source.method {
				continue
			}
			info := analyzeHandler(fn)
			var dataFields []string
			switch source.handler {
			case "AdminBindSubscription", "AdminCreateUserSubscription", "AdminInvalidateUserSubscription", "AdminDeleteUserSubscription":
				dataFields = []string{"message"}
			case "UpdateSubscriptionPreference":
				dataFields = []string{"billing_preference"}
			case "SubscriptionRequestStripePay":
				dataFields = []string{"pay_link"}
			case "SubscriptionRequestCreemPay":
				dataFields = []string{"checkout_url", "order_id"}
			}
			if dataFields != nil {
				properties := map[string]any{}
				for _, name := range dataFields {
					properties[name] = map[string]any{"type": "string"}
				}
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}, "data": map[string]any{"type": "object", "properties": properties}}}
			}
			if source.handler == "AdminCreateSubscriptionPlan" {
				info.RespType = ""
				info.RespSchema = nil
			}
			info.Name = source.handler
			handlers[source.handler] = info
			found = true
		}
		if !found {
			return fmt.Errorf("missing subscription handler %s", source.handler)
		}
	}
	return nil
}

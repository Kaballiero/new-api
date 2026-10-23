package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// Resolve unchanged controller names to the actual payment HTTP and wire declarations.
func parsePaymentsHandlers() error {
	for _, source := range []struct{ handler, scenario string }{
		{"GetTopUpInfo", "get_topup_info"}, {"GetUserTopUps", "list_own_topups"}, {"GetAllTopUps", "admin_list_topups"},
		{"AdminCompleteTopUp", "complete_topup"}, {"RequestAmount", "quote_epay_topup"}, {"RequestEpay", "create_epay_topup"},
		{"EpayNotify", "receive_epay_notify"}, {"RequestStripeAmount", "quote_stripe_topup"}, {"RequestStripePay", "create_stripe_topup"},
		{"StripeWebhook", "receive_stripe_webhook"}, {"RequestCreemPay", "create_creem_topup"}, {"CreemWebhook", "receive_creem_webhook"},
		{"RequestWaffoAmount", "quote_waffo_topup"}, {"RequestWaffoPay", "create_waffo_topup"}, {"WaffoWebhook", "receive_waffo_webhook"},
		{"RequestWaffoPancakeAmount", "quote_pancake_topup"}, {"RequestWaffoPancakePay", "create_pancake_topup"}, {"WaffoPancakeWebhook", "receive_pancake_webhook"},
		{"ListWaffoPancakeCatalog", "list_pancake_catalog"}, {"CreateWaffoPancakePair", "create_pancake_pair"},
		{"SaveWaffoPancake", "save_pancake_config"}, {"ConfirmPaymentCompliance", "confirm_compliance"},
	} {
		dir := filepath.Join("internal/payments", source.scenario)
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
			if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
				continue
			}
			info := analyzeHandler(fn)
			// The published wrapper schemas for the former adaptor entrypoints expose data as string.
			// Keep that contract while request/query facts continue to come from the scenario AST.
			switch source.handler {
			case "RequestStripeAmount", "RequestStripePay", "RequestCreemPay":
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{
					"message": map[string]any{"type": "string"}, "data": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}}
			}

			info.Name = source.handler
			handlers[source.handler] = info
			found = true
		}
		if !found {
			return fmt.Errorf("missing payment handler %s", source.scenario)
		}
	}
	return nil
}

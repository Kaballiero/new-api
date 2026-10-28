package domain

var auditRouteActions = map[string]string{
	"POST /api/user/topup/complete":                    "user.topup_complete",
	"DELETE /api/user/:id/reset_passkey":               "user.reset_passkey",
	"DELETE /api/user/:id/oauth/bindings/:provider_id": "user.oauth_unbind",
	"POST /api/option/payment_compliance":              "option.payment_compliance",
	"POST /api/option/rest_model_ratio":                "option.reset_ratio",
	"DELETE /api/option/channel_affinity_cache":        "option.clear_affinity_cache",
	"POST /api/custom-oauth-provider/":                 "custom_oauth.create",
	"PUT /api/custom-oauth-provider/:id":               "custom_oauth.update",
	"DELETE /api/custom-oauth-provider/:id":            "custom_oauth.delete",
	"DELETE /api/performance/disk_cache":               "performance.clear_disk_cache",
	"POST /api/performance/gc":                         "performance.gc",
	"DELETE /api/performance/logs":                     "performance.clear_logs",
	"PUT /api/redemption/":                             "redemption.update",
	"POST /api/redemption/batch":                       "redemption.delete_batch",
	"DELETE /api/redemption/:id":                       "redemption.delete",
	"DELETE /api/redemption/invalid":                   "redemption.delete_invalid",
	"POST /api/prefill_group/":                         "prefill_group.create",
	"PUT /api/prefill_group/":                          "prefill_group.update",
	"DELETE /api/prefill_group/:id":                    "prefill_group.delete",
	"POST /api/vendors/":                               "vendor.create",
	"PUT /api/vendors/":                                "vendor.update",
	"DELETE /api/vendors/:id":                          "vendor.delete",
	"POST /api/models/":                                "model.create",
	"PUT /api/models/":                                 "model.update",
	"DELETE /api/models/:id":                           "model.delete",
	"POST /api/models/sync_upstream":                   "model.sync_upstream",
	"POST /api/deployments/":                           "deployment.create",
	"PUT /api/deployments/:id":                         "deployment.update",
	"DELETE /api/deployments/:id":                      "deployment.delete",
	"POST /api/subscription/admin/plans":               "subscription.plan_create",
	"PUT /api/subscription/admin/plans/:id":            "subscription.plan_update",
	"POST /api/subscription/admin/bind":                "subscription.bind",
	"POST /api/system-task/log-cleanup":                "log.cleanup_start",
}

func AdminAction(method, route string) string {
	action := auditRouteActions[method+" "+route]
	if action == "" {
		return "generic"
	}
	return action
}

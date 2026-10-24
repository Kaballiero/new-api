// Package contract pins subscription HTTP, persisted state and outgoing provider operations before migration.
package contract

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"database/sql/driver"
	"github.com/Calcium-Ion/go-epay/epay"
	sqliteclock "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	waffoutils "github.com/waffo-com/waffo-go/utils"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const originCommit = "6b0f439a432ab129817e4e41ce323e6bd3c3c0c3"
const testTrade = "WAFFO_PANCAKE_SUB-CONTRACT"
const storeID = "STO_1234567890abcdefghijkl"
const productID = "PROD_1234567890abcdefghijkl"

var manifest = flag.Bool("subscriptions-manifest", false, "print all cases without fixture IO")
var preflight = flag.Bool("subscriptions-preflight", false, "assert HTTP, state and outgoing calls without goldens")
var originCheck = flag.Bool("subscriptions-origin-check", false, "validate the origin recording boundary without writing or fixtures")

// Expected effects are declared before fixtures run; recording cannot bless a missing grant.
type contractCase struct {
	Name, Group, Provider, Actor, Method, Path, Body, Setup, Event, Outcome string
	Status, Orders, Subscriptions, Logs, Debit                              int
	Calls                                                                   []string
	Repeat                                                                  bool
	Next                                                                    string
}

func routes() []contractCase {
	return []contractCase{
		{Name: "plans", Group: "plans", Actor: "user", Method: "GET", Path: "/api/subscription/plans", Status: 200, Outcome: "api"},
		{Name: "self", Group: "self", Actor: "user", Method: "GET", Path: "/api/subscription/self", Status: 200, Outcome: "api"},
		{Name: "preference", Group: "preference", Actor: "user", Method: "PUT", Path: "/api/subscription/self/preference", Body: `{"billing_preference":"wallet_only"}`, Status: 200, Outcome: "api"},
		{Name: "balance", Group: "balance", Actor: "user", Method: "POST", Path: "/api/subscription/balance/pay", Body: `{"plan_id":77}`, Status: 200, Outcome: "api", Orders: 1, Subscriptions: 1, Logs: 1, Debit: 1750000},
		{Name: "epay_pay", Group: "pay", Provider: "epay", Actor: "user", Method: "POST", Path: "/api/subscription/epay/pay", Body: `{"plan_id":77,"payment_method":"alipay"}`, Status: 200, Outcome: "success", Orders: 1},
		{Name: "stripe_pay", Group: "pay", Provider: "stripe", Actor: "user", Method: "POST", Path: "/api/subscription/stripe/pay", Body: `{"plan_id":77}`, Status: 200, Outcome: "success", Orders: 1, Calls: []string{"/v1/checkout/sessions"}},
		{Name: "creem_pay", Group: "pay", Provider: "creem", Actor: "user", Method: "POST", Path: "/api/subscription/creem/pay", Body: `{"plan_id":77}`, Status: 200, Outcome: "success", Orders: 1, Calls: []string{"/v1/checkouts"}},
		{Name: "pancake_pay", Group: "pay", Provider: "pancake", Actor: "user", Method: "POST", Path: "/api/subscription/waffo-pancake/pay", Body: `{"plan_id":77}`, Status: 200, Outcome: "success", Orders: 1, Calls: []string{"/v1/actions/auth/issue-session-token", "/v1/actions/checkout/create-session"}},
		{Name: "admin_plans", Group: "admin_plans", Actor: "admin", Method: "GET", Path: "/api/subscription/admin/plans", Status: 200, Outcome: "api"},
		{Name: "create_plan", Group: "create_plan", Actor: "admin", Method: "POST", Path: "/api/subscription/admin/plans", Body: `{"plan":{"title":"New plan","price_amount":2,"total_amount":500}}`, Status: 200, Outcome: "api"},
		{Name: "update_plan", Group: "update_plan", Actor: "admin", Method: "PUT", Path: "/api/subscription/admin/plans/77", Body: `{"plan":{"title":"Changed plan","price_amount":0,"enabled":false,"total_amount":0}}`, Status: 200, Outcome: "api"},
		{Name: "status", Group: "status", Actor: "admin", Method: "PATCH", Path: "/api/subscription/admin/plans/77", Body: `{"enabled":false}`, Status: 200, Outcome: "api"},
		{Name: "bind", Group: "bind", Actor: "admin", Method: "POST", Path: "/api/subscription/admin/bind", Body: `{"user_id":1,"plan_id":77}`, Status: 200, Outcome: "api", Subscriptions: 1},
		{Name: "reset_plan", Group: "reset_plan", Actor: "admin", Method: "POST", Path: "/api/subscription/admin/plans/77/subscriptions/reset", Body: `{}`, Status: 200, Outcome: "api", Subscriptions: 3},
		{Name: "admin_user_list", Group: "admin_user_list", Actor: "admin", Method: "GET", Path: "/api/subscription/admin/users/1/subscriptions", Status: 200, Outcome: "api"},
		{Name: "create_user", Group: "create_user", Actor: "admin", Method: "POST", Path: "/api/subscription/admin/users/1/subscriptions", Body: `{"plan_id":77}`, Status: 200, Outcome: "api", Subscriptions: 1},
		{Name: "reset_user", Group: "reset_user", Actor: "admin", Method: "POST", Path: "/api/subscription/admin/users/1/subscriptions/reset", Body: `{"plan_id":77}`, Status: 200, Outcome: "api", Subscriptions: 3},
		{Name: "invalidate", Group: "invalidate", Actor: "admin", Method: "POST", Path: "/api/subscription/admin/user_subscriptions/11/invalidate", Status: 200, Outcome: "api", Subscriptions: 3},
		{Name: "delete", Group: "delete", Actor: "admin", Method: "DELETE", Path: "/api/subscription/admin/user_subscriptions/11", Status: 200, Outcome: "api", Subscriptions: 2},
		{Name: "product", Group: "product", Provider: "pancake", Actor: "root", Method: "POST", Path: "/api/option/waffo-pancake/subscription-product", Body: `{"name":" New product ","amount":" 3.50 "}`, Status: 200, Outcome: "success", Calls: []string{"/v1/actions/onetime-product/create-product", "/v1/actions/onetime-product/publish-product"}},
		{Name: "products", Group: "products", Provider: "pancake", Actor: "root", Method: "GET", Path: "/api/option/waffo-pancake/subscription-product-options", Status: 200, Outcome: "success", Calls: []string{"/v1/graphql"}},
	}
}

func contractCases() []contractCase {
	cases := []contractCase{}
	for _, base := range routes() {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "api_key"} {
			tc := base
			tc.Name = "access_" + base.Name + "_" + actor
			tc.Actor = actor
			denied := actor == "anonymous" || actor == "api_key"
			if denied {
				tc.Status = 401
			} else if (base.Actor == "admin" && strings.HasPrefix(actor, "user")) || (base.Actor == "root" && !strings.HasPrefix(actor, "root")) {
				tc.Status = 403
				denied = true
			}
			if denied {
				tc.Outcome = "denied"
				tc.Orders = 0
				tc.Logs = 0
				tc.Debit = 0
				tc.Calls = nil
				tc.Subscriptions = 0
				if seededGroup(base.Group) {
					tc.Subscriptions = 3
				}
			}
			cases = append(cases, tc)
		}
	}
	for _, base := range routes() {
		if base.Group == "pay" || base.Group == "balance" || base.Group == "bind" || base.Group == "create_user" {
			for _, variant := range []string{"malformed", "zero_id", "missing_plan", "disabled_plan", "compliance_off", "purchase_limit", "expired_limit", "group_upgrade", "insert_failure", "user_missing"} {
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				reject := variant != "group_upgrade" && !(variant == "disabled_plan" && (base.Group == "bind" || base.Group == "create_user"))
				if reject {
					tc.Outcome = "api_error"
					if base.Group == "pay" && ((base.Provider == "creem" && (variant == "malformed" || variant == "zero_id")) || (base.Provider != "epay" && variant == "insert_failure")) {
						tc.Outcome = "error"
					}
					tc.Subscriptions = 0
					tc.Logs = 0
					tc.Orders = 0
					tc.Debit = 0
					tc.Calls = nil
					if base.Provider == "stripe" && variant == "insert_failure" {
						tc.Calls = base.Calls
					}
				}
				if variant == "purchase_limit" || variant == "expired_limit" {
					tc.Subscriptions = 1
				}
				// Session authentication rejects a deleted actor before protected purchase handlers run.
				if variant == "user_missing" && (base.Group == "pay" || base.Group == "balance") {
					tc.Status, tc.Outcome = 401, "denied"
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "pay" {
			for _, variant := range []string{"provider_disabled", "upstream_error", "upstream_malformed", "upstream_empty", "existing_customer", "empty_email", "gateway_product_missing", "webhook_missing", "price_zero", "price_tiny"} {
				if base.Provider == "epay" && strings.HasPrefix(variant, "upstream_") {
					continue
				}
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				reject := variant == "provider_disabled" || variant == "webhook_missing" || strings.HasPrefix(variant, "upstream_") || (base.Provider == "epay" && (variant == "price_zero" || variant == "price_tiny"))
				if base.Provider == "pancake" && variant == "webhook_missing" {
					reject = false
				}
				if base.Provider == "epay" && variant == "webhook_missing" {
					reject = false
				}
				if base.Provider == "stripe" && variant == "upstream_empty" {
					reject = false
				}
				if reject {
					tc.Outcome = "api_error"
					tc.Orders = 0
					tc.Calls = nil
					if strings.HasPrefix(variant, "upstream_") {
						tc.Outcome = "error"
						tc.Calls = base.Calls
						if base.Provider == "creem" || base.Provider == "pancake" {
							tc.Orders = 1
						}
					}
				}
				cases = append(cases, tc)
			}
		}
	}
	for _, variant := range []string{"balance_disabled", "insufficient", "exact_balance", "free_plan", "fraction_price", "negative_price", "invalid_quota_unit", "grant_failure", "order_failure", "commit_failure"} {
		tc := routes()[3]
		tc.Name = "balance_" + variant
		tc.Setup = variant
		if variant == "free_plan" {
			tc.Debit = 0
		} else if variant == "fraction_price" {
			tc.Debit = 1
		} else if variant != "exact_balance" {
			tc.Outcome = "api_error"
			tc.Orders = 0
			tc.Subscriptions = 0
			tc.Logs = 0
			tc.Debit = 0
		}
		cases = append(cases, tc)
	}
	for _, base := range routes() {
		if base.Group == "create_plan" || base.Group == "update_plan" {
			for _, variant := range []string{"malformed", "empty_title", "negative_price", "high_price", "negative_limit", "negative_total", "unknown_upgrade", "unknown_downgrade", "custom_reset_zero", "currency_forced", "flags_false", "duration_custom", "unknown_reset", "missing_id", "compliance_off"} {
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				if !slices.Contains([]string{"currency_forced", "flags_false", "duration_custom", "unknown_reset", "missing_id"}, variant) {
					tc.Outcome = "api_error"
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "status" {
			for _, variant := range []string{"malformed", "missing_enabled", "enabled_true", "missing_id", "compliance_off"} {
				tc := base
				tc.Name = "status_" + variant
				tc.Setup = variant
				if variant != "enabled_true" && variant != "missing_id" {
					tc.Outcome = "api_error"
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "self" || base.Group == "admin_user_list" {
			for _, variant := range []string{"multiple", "query_ignored", "setting_error", "all_error", "active_error"} {
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				if variant == "multiple" || variant == "query_ignored" || variant == "active_error" {
					tc.Subscriptions = 3
				}
				if base.Group == "admin_user_list" && (variant == "all_error") {
					tc.Outcome = "api_error"
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "plans" || base.Group == "admin_plans" {
			for _, variant := range []string{"compliance_off", "query_ignored", "query_failure"} {
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				if variant == "query_failure" {
					tc.Outcome = "api_error"
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "preference" {
			for _, value := range []string{"subscription_first", "wallet_first", "subscription_only", "wallet_only", " wallet_only ", "unknown", ""} {
				tc := base
				tc.Name = "preference_value_" + strconv.Itoa(len(cases))
				tc.Body = `{"billing_preference":` + strconv.Quote(value) + `}`
				cases = append(cases, tc)
			}
			for _, variant := range []string{"malformed", "wrong_type", "write_failure"} {
				tc := base
				tc.Name = "preference_" + variant
				tc.Setup = variant
				tc.Outcome = "api_error"
				cases = append(cases, tc)
			}
		}
		if seededGroup(base.Group) {
			for _, variant := range []string{"malformed", "missing_id", "compliance_off", "keep_reset", "no_matches", "downgrade_explicit", "other_upgrade", "write_failure"} {
				if (base.Group == "invalidate" || base.Group == "delete") && (variant == "malformed" || variant == "keep_reset" || variant == "no_matches") {
					continue
				}
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				if variant == "malformed" || variant == "missing_id" || variant == "write_failure" || (base.Group == "reset_user" && variant == "no_matches") {
					tc.Outcome = "api_error"
					tc.Subscriptions = 3
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "product" || base.Group == "products" {
			for _, variant := range []string{"compliance_off", "provider_disabled", "upstream_error", "upstream_malformed", "inactive_products", "empty_catalog", "orphan_publish"} {
				if base.Group == "products" && variant == "orphan_publish" {
					continue
				}
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				if variant == "provider_disabled" || strings.HasPrefix(variant, "upstream_") || variant == "orphan_publish" {
					tc.Outcome = "error"
					if variant == "provider_disabled" {
						tc.Calls = nil
					} else if base.Group == "product" && variant != "orphan_publish" {
						tc.Calls = base.Calls[:1]
					}
				}
				cases = append(cases, tc)
			}
		}
	}
	for _, provider := range []string{"epay", "stripe", "creem", "pancake"} {
		methods := []string{"POST"}
		if provider == "epay" {
			methods = []string{"POST", "GET"}
		}
		for _, method := range methods {
			for _, kind := range []string{"notify", "return"} {
				if provider != "epay" && kind == "return" {
					continue
				}
				tc := contractCase{Name: provider + "_" + method + "_" + kind, Group: "webhook", Provider: provider, Method: method, Status: 200, Outcome: "empty", Orders: 1, Subscriptions: 1, Logs: 1, Event: "paid"}
				switch provider {
				case "epay":
					tc.Path = "/api/subscription/epay/" + kind
					tc.Outcome = "success"
					if kind == "return" {
						tc.Status = 302
						tc.Outcome = "redirect_success"
					}
				case "stripe":
					tc.Path = "/api/stripe/webhook"
				case "creem":
					tc.Path = "/api/creem/webhook"
				case "pancake":
					tc.Path = "/api/waffo-pancake/webhook/test"
					tc.Outcome = "OK"
				}
				tc.Repeat = true
				cases = append(cases, tc)
				for _, variant := range []string{"bad_signature", "missing_signature", "unknown_order", "foreign_provider", "already_paid", "failed_order", "disabled_plan", "purchase_limit", "missing_plan", "user_missing", "method_switch", "wrong_money", "unpaid", "compliance_off", "post_query_ignored", "commit_failure", "history_conflict"} {
					if provider != "epay" && (variant == "method_switch" || variant == "post_query_ignored") || method == "GET" && variant == "post_query_ignored" {
						continue
					}
					v := tc
					v.Name = tc.Name + "_" + variant
					v.Setup = variant
					v.Repeat = false
					reject := slices.Contains([]string{"bad_signature", "missing_signature", "unknown_order", "foreign_provider", "already_paid", "failed_order", "purchase_limit", "missing_plan", "user_missing", "unpaid", "commit_failure", "history_conflict"}, variant)
					if variant == "compliance_off" && provider != "epay" {
						reject = true
						v.Status = 403
						v.Outcome = "empty"
						if provider == "pancake" {
							v.Outcome = "webhook disabled"
						}
					}
					if reject {
						v.Subscriptions = 0
						v.Logs = 0
						if variant == "purchase_limit" {
							v.Subscriptions = 1
						}
						if variant == "unknown_order" {
							v.Orders = 0
						}
						switch provider {
						case "epay":
							v.Outcome = "fail"
							if kind == "return" {
								v.Outcome = "redirect_fail"
								if variant == "unpaid" {
									v.Outcome = "redirect_pending"
								}
							}
						case "stripe":
							if variant == "bad_signature" || variant == "missing_signature" {
								v.Status = 400
							}
						case "creem":
							if variant == "bad_signature" || variant == "missing_signature" {
								v.Status = 401
							} else if variant == "unknown_order" {
								v.Status = 400
							} else if variant != "unpaid" {
								v.Status = 500
							}
						case "pancake":
							if variant == "bad_signature" || variant == "missing_signature" {
								v.Status = 401
								v.Outcome = "invalid signature"
							} else if !slices.Contains([]string{"unknown_order", "foreign_provider", "unpaid"}, variant) {
								v.Status = 500
								v.Outcome = "retry"
							}
						}
					}
					if variant == "unpaid" {
						v.Event = "unpaid"
						if provider == "pancake" {
							v.Subscriptions = 1
							v.Logs = 1
						}
					}
					if variant == "already_paid" {
						v.Subscriptions = 1
						v.Status, v.Outcome = tc.Status, tc.Outcome
					}
					if variant == "compliance_off" && provider != "epay" {
						v.Status, v.Outcome = 403, "empty"
						if provider == "pancake" {
							v.Outcome = "webhook disabled"
						}
					}
					cases = append(cases, v)
				}
				if provider == "stripe" {
					for _, event := range []string{"async_success", "async_failed", "expired", "expired_wrong_status", "unknown"} {
						v := tc
						v.Name = tc.Name + "_" + event
						v.Event = event
						v.Repeat = false
						if event != "async_success" {
							v.Subscriptions = 0
							v.Logs = 0
						}
						cases = append(cases, v)
					}
				}
				if provider == "pancake" {
					for _, variant := range []string{"buyer_mismatch", "wrong_env", "stale_signature"} {
						v := tc
						v.Name = tc.Name + "_" + variant
						v.Setup = variant
						v.Repeat = false
						v.Subscriptions = 0
						v.Logs = 0
						if variant == "stale_signature" {
							v.Status = 401
							v.Outcome = "invalid signature"
						}
						cases = append(cases, v)
					}
				}
				if provider == "creem" {
					v := tc
					v.Name = tc.Name + "_not_onetime"
					v.Setup = "not_onetime"
					cases = append(cases, v)
				}
			}
		}
	}
	readFailure := routes()[6]
	readFailure.Name, readFailure.Setup, readFailure.Outcome = "creem_pay_read_failure", "read_failure", "error"
	readFailure.Orders, readFailure.Calls = 0, nil
	cases = append(cases, readFailure)
	limited := routes()[4]
	limited.Name, limited.Setup, limited.Status, limited.Outcome = "epay_pay_critical_rate", "critical_rate", 429, "empty"
	limited.Orders, limited.Calls = 0, nil
	cases = append(cases, limited)
	for _, path := range []string{"/api/subscription/epay/notify", "/api/subscription/epay/return", "/api/stripe/webhook", "/api/creem/webhook", "/api/waffo-pancake/webhook/test"} {
		provider := "epay"
		if strings.Contains(path, "stripe") {
			provider = "stripe"
		}
		if strings.Contains(path, "creem") {
			provider = "creem"
		}
		if strings.Contains(path, "pancake") {
			provider = "pancake"
		}
		cases = append(cases, contractCase{Name: "body_limit_" + provider + "_" + strings.ReplaceAll(path, "/", "_"), Group: "webhook", Provider: provider,
			Method: "POST", Path: path, Setup: "body_limit", Event: "paid", Status: 413, Outcome: "empty", Orders: 1})
	}
	return cases
}

func seededGroup(group string) bool {
	return group == "reset_user" || group == "reset_plan" || group == "invalidate" || group == "delete"
}

func localeCases() []string {
	return []string{"access_plans_user", "access_self_user", "access_balance_user", "balance_insufficient", "epay_pay_compliance_off", "epay_pay_missing_plan", "epay_pay_disabled_plan", "epay_pay_purchase_limit", "stripe_pay_provider_disabled", "creem_pay_malformed", "creem_pay_read_failure", "pancake_pay_provider_disabled", "create_plan_empty_title", "create_plan_negative_price", "create_plan_high_price", "create_plan_unknown_upgrade", "create_plan_custom_reset_zero", "status_missing_enabled", "reset_user_no_matches", "access_reset_plan_admin", "access_bind_admin", "product_provider_disabled"}
}

func TestSubscriptionsContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if *originCheck {
		require.False(t, updating)
		verifyOriginRecording(t)
		return
	}
	if updating {
		verifyOriginRecording(t)
	}
	cases := contractCases()
	seen := map[string]bool{}
	for _, tc := range cases {
		require.False(t, seen[tc.Name], tc.Name)
		seen[tc.Name] = true
		require.NotZero(t, tc.Status, tc.Name)
	}
	if *manifest {
		require.False(t, updating)
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("SUBSCRIPTIONS_MANIFEST " + string(data))
		data, err = common.Marshal(localeCases())
		require.NoError(t, err)
		fmt.Println("SUBSCRIPTIONS_LOCALES " + string(data))
		return
	}
	if name := os.Getenv("SUBSCRIPTIONS_CONTRACT_CASE"); name != "" {
		for _, tc := range cases {
			if tc.Name == name {
				runContractCase(t, tc, os.Getenv("SUBSCRIPTIONS_CONTRACT_LANGUAGE"))
				return
			}
		}
		t.Fatalf("unknown subscriptions case %q", name)
	}
	if path := os.Getenv("SUBSCRIPTIONS_CONTRACT_LOCALE_EVIDENCE"); path != "" {
		require.False(t, updating)
		writeLocaleEvidence(t, path)
		return
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			output := runChild(t, tc.Name, "zh-CN", updating, *preflight)
			if *preflight {
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, "SUBSCRIPTIONS_SNAPSHOT ") {
						fmt.Println(line)
					}
				}
			}
		})
	}
}

const dbEpoch int64 = 1893456000

func configureCase(t *testing.T, f *fixture, tc contractCase) {
	t.Helper()
	require.Equal(t, dbEpoch, model.GetDBTimestamp())
	require.NoError(t, f.env.DB.Model(&model.User{}).Where("id IN ?", []int{1, 2, 3}).Update("quota", 5000000).Error)
	plan := model.SubscriptionPlan{Id: 77, Title: "Contract plan", Subtitle: "Subscription fixture", PriceAmount: 3.5, Currency: "USD", DurationUnit: "month", DurationValue: 1, Enabled: true, TotalAmount: 1000, StripePriceId: "price_subscription", CreemProductId: "prod_subscription", WaffoPancakeProductId: productID, QuotaResetPeriod: "custom", QuotaResetCustomSeconds: 3600}
	require.NoError(t, f.env.DB.Create(&plan).Error)
	disabled := plan
	disabled.Id = 78
	disabled.Title = "Disabled plan"
	disabled.SortOrder = 10
	require.NoError(t, f.env.DB.Create(&disabled).Error)
	require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 78).Update("enabled", false).Error)
	seed := seededGroup(tc.Group) || tc.Setup == "multiple" || tc.Setup == "query_ignored" && (tc.Group == "self" || tc.Group == "admin_user_list") || tc.Setup == "active_error"
	if seed {
		require.NoError(t, f.env.DB.Create(&[]model.UserSubscription{
			{Id: 11, UserId: 1, PlanId: 77, AmountTotal: 1000, AmountUsed: 400, StartTime: dbEpoch - 7200, EndTime: 4102444800, Status: "active", LastResetTime: dbEpoch - 7200, NextResetTime: dbEpoch - 3600, UpgradeGroup: "pro", PrevUserGroup: "default"},
			{Id: 12, UserId: 1, PlanId: 77, AmountTotal: 1000, AmountUsed: 600, StartTime: 100, EndTime: 101, Status: "expired"},
			{Id: 13, UserId: 2, PlanId: 77, AmountTotal: 1000, AmountUsed: 300, StartTime: dbEpoch - 3600, EndTime: 4102444801, Status: "active", LastResetTime: dbEpoch, NextResetTime: dbEpoch + 3600},
		}).Error)
	}
	if tc.Setup == "purchase_limit" || tc.Setup == "expired_limit" {
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("max_purchase_per_user", 1).Error)
		status := "active"
		end := int64(4102444800)
		if tc.Setup == "expired_limit" {
			status = "expired"
			end = 100
		}
		userID := actorID(tc.Actor)
		if tc.Group == "bind" || tc.Group == "create_user" || tc.Group == "webhook" {
			userID = 1
		}
		require.NoError(t, f.env.DB.Create(&model.UserSubscription{Id: 11, UserId: userID, PlanId: 77, Status: status, StartTime: 1, EndTime: end}).Error)
	}
	switch tc.Setup {
	case "compliance_off":
		operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	case "disabled_plan":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("enabled", false).Error)
	case "group_upgrade":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("upgrade_group", "pro").Error)
	case "balance_disabled":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("allow_balance_pay", false).Error)
	case "insufficient":
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", 1).Error)
	case "exact_balance":
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", 1750000).Error)
	case "free_plan", "price_zero":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("price_amount", 0).Error)
	case "price_tiny":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("price_amount", 0.001).Error)
	case "fraction_price":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("price_amount", 0.000001).Error)
	case "negative_price":
		require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("price_amount", -1).Error)
	case "invalid_quota_unit":
		common.QuotaPerUnit = 0
	case "provider_disabled":
		switch tc.Provider {
		case "epay":
			operation_setting.EpayKey = ""
		case "stripe":
			setting.StripeApiSecret = "invalid"
		case "creem":
			require.NoError(t, f.env.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", 77).Update("creem_product_id", "").Error)
		case "pancake":
			setting.WaffoPancakeMerchantID = ""
		}
	case "webhook_missing":
		setting.StripeWebhookSecret = ""
		setting.CreemWebhookSecret = ""
	case "gateway_product_missing":
		setting.WaffoPancakeProductID = ""
	case "existing_customer":
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("stripe_customer", "cus_existing").Error)
	case "user_missing":
		require.NoError(t, f.env.DB.Where("id = ?", 1).Delete(&model.User{}).Error)
	case "missing_plan":
		require.NoError(t, f.env.DB.Where("id = ?", 77).Delete(&model.SubscriptionPlan{}).Error)
	case "downgrade_explicit":
		require.NoError(t, f.env.DB.Model(&model.UserSubscription{}).Where("id = ?", 11).Update("downgrade_group", "fallback").Error)
	case "other_upgrade":
		require.NoError(t, f.env.DB.Model(&model.UserSubscription{}).Where("id = ?", 12).Updates(map[string]any{"status": "active", "end_time": int64(4102444802), "upgrade_group": "other"}).Error)
	case "no_matches":
		require.NoError(t, f.env.DB.Model(&model.UserSubscription{}).Where("1=1").UpdateColumn("status", "expired").Error)
	}
	if tc.Group == "webhook" {
		provider := tc.Provider
		if provider == "pancake" {
			provider = "waffo_pancake"
		}
		if tc.Setup == "foreign_provider" {
			provider = "foreign"
		}
		status := "pending"
		if tc.Setup == "already_paid" {
			status = "success"
		}
		if tc.Setup == "failed_order" {
			status = "failed"
		}
		if tc.Setup != "unknown_order" {
			require.NoError(t, f.env.DB.Create(&model.SubscriptionOrder{Id: 21, UserId: 1, PlanId: 77, Money: 3.5, TradeNo: f.trade, PaymentMethod: tc.Provider, PaymentProvider: provider, CreateTime: 100, Status: status}).Error)
		}
		if tc.Setup == "already_paid" {
			require.NoError(t, f.env.DB.Create(&model.UserSubscription{Id: 11, UserId: 1, PlanId: 77, Status: "active", StartTime: dbEpoch, EndTime: 4102444800}).Error)
		}
		if tc.Setup == "history_conflict" {
			require.NoError(t, f.env.DB.Create(&model.TopUp{Id: 31, UserId: 1, TradeNo: f.trade, PaymentMethod: "foreign", Status: "pending"}).Error)
		}
	}
	model.InvalidateSubscriptionPlanCache(77)
	if tc.Setup == "query_failure" || tc.Setup == "all_error" || tc.Setup == "active_error" || tc.Setup == "setting_error" {
		require.NoError(t, f.env.DB.Callback().Query().Before("gorm:query").Register("subscriptions_query_failure", func(tx *gorm.DB) {
			reject := tc.Setup == "query_failure" && tx.Statement.Table == "subscription_plans" || tc.Setup == "all_error" && tx.Statement.Table == "user_subscriptions" || tc.Setup == "active_error" && tx.Statement.Table == "user_subscriptions" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"]), "status") || tc.Setup == "setting_error" && tx.Statement.Table == "users" && slices.Contains(tx.Statement.Selects, "setting")
			if reject {
				tx.AddError(fmt.Errorf("fixture query failure"))
			}
		}))
	}
	if slices.Contains([]string{"insert_failure", "grant_failure", "order_failure"}, tc.Setup) {
		require.NoError(t, f.env.DB.Callback().Create().Before("gorm:create").Register("subscriptions_insert_failure", func(tx *gorm.DB) {
			table := tx.Statement.Table
			if tc.Setup == "insert_failure" && (table == "subscription_orders" || table == "user_subscriptions") || tc.Setup == "grant_failure" && table == "user_subscriptions" || tc.Setup == "order_failure" && table == "subscription_orders" {
				tx.AddError(fmt.Errorf("fixture insert failure"))
			}
		}))
	}
	if tc.Setup == "write_failure" {
		require.NoError(t, f.env.DB.Callback().Update().Before("gorm:update").Register("subscriptions_update_failure", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("fixture update failure")) }))
		require.NoError(t, f.env.DB.Callback().Delete().Before("gorm:delete").Register("subscriptions_delete_failure", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("fixture delete failure")) }))
	}
	if tc.Setup == "commit_failure" {
		require.NoError(t, f.env.DB.Callback().Create().After("gorm:create").Register("subscriptions_commit_failure", func(tx *gorm.DB) {
			if tc.Group == "balance" && tx.Statement.Table == "subscription_orders" {
				active, ok := tx.Statement.ConnPool.(gorm.TxCommitter)
				if assert.True(t, ok, "fixture requires an active transaction") {
					assert.NoError(t, active.Rollback())
					f.commitRollbacks++
				}
			}
		}))
		require.NoError(t, f.env.DB.Callback().Update().After("gorm:update").Register("subscriptions_order_commit_failure", func(tx *gorm.DB) {
			if tc.Group == "webhook" && tx.Statement.Table == "subscription_orders" {
				active, ok := tx.Statement.ConnPool.(gorm.TxCommitter)
				if assert.True(t, ok, "fixture requires an active transaction") {
					assert.NoError(t, active.Rollback())
					f.commitRollbacks++
				}
			}
		}))
	}
}

func actorID(actor string) int {
	if strings.HasPrefix(actor, "admin") {
		return 2
	}
	if strings.HasPrefix(actor, "root") {
		return 3
	}
	return 1
}

func requestBody(tc contractCase) string {
	if tc.Setup == "malformed" {
		return "{"
	}
	if tc.Setup == "zero_id" {
		if tc.Provider == "epay" {
			return `{"plan_id":0,"payment_method":"alipay"}`
		}
		return `{"plan_id":0}`
	}
	if tc.Setup == "wrong_type" {
		return `{"billing_preference":true}`
	}
	if tc.Setup == "keep_reset" {
		return `{"plan_id":77,"advance_reset_time":false}`
	}
	if tc.Setup == "missing_enabled" {
		return `{}`
	}
	if tc.Setup == "enabled_true" {
		return `{"enabled":true}`
	}
	if tc.Group == "create_plan" || tc.Group == "update_plan" {
		fields := map[string]any{"title": "Changed plan", "price_amount": 2, "total_amount": 500}
		switch tc.Setup {
		case "empty_title":
			fields["title"] = " "
		case "negative_price":
			fields["price_amount"] = -1
		case "high_price":
			fields["price_amount"] = 10000
		case "negative_limit":
			fields["max_purchase_per_user"] = -1
		case "negative_total":
			fields["total_amount"] = -1
		case "unknown_upgrade":
			fields["upgrade_group"] = "absent"
		case "unknown_downgrade":
			fields["downgrade_group"] = "absent"
		case "custom_reset_zero":
			fields["quota_reset_period"] = "custom"
		case "currency_forced":
			fields["currency"] = "EUR"
		case "flags_false":
			fields["allow_balance_pay"] = false
			fields["allow_wallet_overflow"] = false
		case "duration_custom":
			fields["duration_unit"] = "custom"
			fields["custom_seconds"] = 7200
		case "unknown_reset":
			fields["quota_reset_period"] = "unknown"
		}
		data, _ := common.Marshal(map[string]any{"plan": fields})
		return string(data)
	}
	return tc.Body
}

type httpSnapshot struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}
type walletRow struct {
	ID             int    `json:"id"`
	Quota          int    `json:"quota"`
	Group          string `json:"group"`
	Setting        string `json:"setting"`
	Email          string `json:"email"`
	StripeCustomer string `json:"stripe_customer"`
	AuthVersion    int64  `json:"auth_version"`
}
type stepSnapshot struct {
	HTTP          httpSnapshot                         `json:"http"`
	Plans         []model.SubscriptionPlan             `json:"plans"`
	Orders        []model.SubscriptionOrder            `json:"orders"`
	Subscriptions []model.UserSubscription             `json:"subscriptions"`
	Ledger        []model.SubscriptionPreConsumeRecord `json:"ledger"`
	History       []model.TopUp                        `json:"history"`
	Wallets       []walletRow                          `json:"wallets"`
	Logs          []model.Log                          `json:"logs"`
	Events        []model.AuditLog                     `json:"events"`
	Outbound      []outbound                           `json:"outbound"`
	Options       []model.Option                       `json:"options"`
}

func runContractCase(t *testing.T, tc contractCase, language string) {
	f := newFixture(t, tc)
	if language == "" {
		language = "zh-CN"
	}
	if tc.Setup == "no_matches" {
		require.NoError(t, f.env.DB.Model(&model.UserSubscription{}).Where("1=1").UpdateColumn("status", "expired").Error)
	}
	if tc.Setup == "critical_rate" {
		for range 20 {
			r := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader("{"))
			r.Header.Set("Authorization", "Bearer "+f.bearers[tc.Actor])
			r.Header.Set("Content-Type", "application/json")
			r.RemoteAddr = "192.0.2.8:3456"
			require.Equal(t, 200, f.env.Serve(r).Code)
		}
	}
	before := []walletRow{}
	require.NoError(t, f.env.DB.Model(&model.User{}).Order("id").Find(&before).Error)
	steps := []stepSnapshot{}
	requestIDs := map[string]string{}
	repeats := 1
	if tc.Repeat {
		repeats = 2
	}
	for step := range repeats {
		var r *http.Request
		if tc.Group == "webhook" {
			r = webhookRequest(t, f, tc, step)
		} else {
			path := tc.Path
			if tc.Setup == "missing_id" {
				path = strings.Replace(path, "/77", "/999", 1)
				path = strings.Replace(path, "/11", "/999", 1)
				path = strings.Replace(path, "/users/1/", "/users/999/", 1)
			}
			if tc.Setup == "query_ignored" {
				path += "?p=99&page_size=1&user_id=999&status=disabled&sort=asc"
			}
			r = httptest.NewRequest(tc.Method, path, strings.NewReader(requestBody(tc)))
			r.Header.Set("Content-Type", "application/json")
		}
		if language != "default" {
			r.Header.Set("Accept-Language", language)
		}
		if bearer := f.bearers[tc.Actor]; bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		r.Header.Set("User-Agent", "subscriptions-contract-agent")
		r.RemoteAddr = "192.0.2.8:3456"
		if tc.Setup == "read_failure" && tc.Group == "pay" {
			r.Body = failingBody{}
			r.ContentLength = -1
		}
		if tc.Setup == "body_limit" {
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 512*1024+1)))
			r.ContentLength = 512*1024 + 1
		}
		started := time.Now().Unix()
		response := f.env.Serve(r)
		require.Equal(t, tc.Status, response.Code, response.Body.String())
		if tc.Setup == "critical_rate" {
			require.Equal(t, "1200", response.Header().Get("Retry-After"))
		}
		if tc.Setup == "commit_failure" {
			require.Equal(t, 1, f.commitRollbacks, "commit must be attempted after the fixture rolls back the final write")
		}
		if tc.Group == "pay" && tc.Provider == "epay" && tc.Outcome == "success" {
			var out struct {
				Data map[string]string `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &out))
			actualSignature := out.Data["sign"]
			require.NotEmpty(t, actualSignature)
			require.Equal(t, actualSignature, epay.GenerateParams(out.Data, operation_setting.EpayKey)["sign"])
		}
		switch tc.Outcome {
		case "api", "api_error", "denied":
			var out struct {
				Success *bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &out))
			require.NotNil(t, out.Success, response.Body.String())
			require.Equal(t, tc.Outcome == "api", *out.Success, response.Body.String())
		case "success", "error":
			if tc.Group == "webhook" {
				require.Equal(t, tc.Outcome, response.Body.String())
			} else {
				var out struct {
					Message string `json:"message"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &out))
				require.Equal(t, tc.Outcome, out.Message, response.Body.String())
			}
		case "empty":
			require.Empty(t, response.Body.String())
		case "redirect_success", "redirect_fail", "redirect_pending":
			require.Equal(t, "https://console.example.test/wallet?pay="+strings.TrimPrefix(tc.Outcome, "redirect_"), response.Header().Get("Location"))
		default:
			require.Equal(t, tc.Outcome, response.Body.String())
		}
		state := stepSnapshot{HTTP: httpSnapshot{response.Code, map[string]string{}, response.Body.String()}, Plans: []model.SubscriptionPlan{}, Orders: []model.SubscriptionOrder{}, Subscriptions: []model.UserSubscription{}, Ledger: []model.SubscriptionPreConsumeRecord{}, History: []model.TopUp{}, Wallets: []walletRow{}, Logs: []model.Log{}, Events: []model.AuditLog{}, Outbound: []outbound{}, Options: []model.Option{}}
		for _, key := range []string{"Content-Type", "Cache-Control", "Retry-After", "Location"} {
			if value := response.Header().Get(key); value != "" {
				state.HTTP.Headers[key] = value
			}
		}
		if id := response.Header().Get(common.RequestIdKey); id != "" {
			requestIDs[id] = fmt.Sprintf("<request-%d>", step+1)
		}
		// Fault callbacks protect the request only; observation must always read the actual persisted rows.
		if step == 0 && slices.Contains([]string{"query_failure", "all_error", "active_error", "setting_error"}, tc.Setup) {
			require.NoError(t, f.env.DB.Callback().Query().Remove("subscriptions_query_failure"))
		}
		require.NoError(t, f.env.DB.Order("id").Find(&state.Plans).Error)
		require.NoError(t, f.env.DB.Order("id").Find(&state.Orders).Error)
		require.NoError(t, f.env.DB.Order("id").Find(&state.Subscriptions).Error)
		require.NoError(t, f.env.DB.Order("id").Find(&state.Ledger).Error)
		require.NoError(t, f.env.DB.Order("id").Find(&state.History).Error)
		require.NoError(t, f.env.DB.Model(&model.User{}).Order("id").Find(&state.Wallets).Error)
		require.NoError(t, f.log.Order("id").Find(&state.Logs).Error)
		require.NoError(t, f.log.Order("id").Find(&state.Events).Error)
		require.NoError(t, f.env.DB.Order("key").Find(&state.Options).Error)
		require.Len(t, state.Orders, tc.Orders)
		require.Len(t, state.Subscriptions, tc.Subscriptions)
		require.Len(t, state.Logs, tc.Logs)
		wantEvents := 0
		if strings.HasSuffix(tc.Actor, "_pat") {
			wantEvents++
		}
		management := (strings.HasPrefix(tc.Actor, "admin") || strings.HasPrefix(tc.Actor, "root")) &&
			tc.Method != "GET" && tc.Status != 401 && tc.Status != 403 &&
			slices.Contains([]string{"create_plan", "update_plan", "status", "bind", "create_user", "reset_user", "reset_plan", "invalidate", "delete", "product"}, tc.Group)
		if management {
			wantEvents++
			if tc.Outcome == "api" && tc.Group == "reset_user" {
				wantEvents++
			}
			if tc.Outcome == "api" && tc.Group == "reset_plan" && tc.Setup != "no_matches" {
				wantEvents += 2
			}
		}
		require.Len(t, state.Events, wantEvents*(step+1), "recipient/management/PAT audit count")
		require.Empty(t, state.Ledger)
		require.Empty(t, state.Options)
		for _, wallet := range state.Wallets {
			for _, old := range before {
				if wallet.ID == old.ID {
					debit := 0
					if wallet.ID == actorID(tc.Actor) {
						debit = tc.Debit
					}
					require.Equal(t, old.Quota-debit, wallet.Quota)
					require.Equal(t, old.AuthVersion, wallet.AuthVersion)
				}
			}
		}
		if tc.Group == "balance" && tc.Outcome == "api" {
			require.Empty(t, state.History)
			require.Equal(t, "charged_quota="+strconv.Itoa(tc.Debit), state.Orders[0].ProviderPayload)
			require.Equal(t, "balance", state.Orders[0].PaymentMethod)
		}
		if tc.Group == "webhook" && tc.Logs > 0 {
			require.Len(t, state.History, 1)
			require.Zero(t, state.History[0].Amount)
			require.Equal(t, "", state.History[0].PaymentProvider)
			require.Equal(t, "success", state.Orders[0].Status)
		}
		if tc.Group == "pay" && tc.Outcome == "success" {
			require.Equal(t, "pending", state.Orders[0].Status)
		}
		if tc.Group == "reset_user" && tc.Outcome == "api" {
			require.Zero(t, state.Subscriptions[0].AmountUsed)
			if tc.Setup == "other_upgrade" {
				require.Zero(t, state.Subscriptions[1].AmountUsed)
			} else {
				require.EqualValues(t, 600, state.Subscriptions[1].AmountUsed)
			}
			require.EqualValues(t, 300, state.Subscriptions[2].AmountUsed)
		}
		if tc.Group == "reset_plan" && tc.Outcome == "api" && tc.Setup != "no_matches" {
			require.Zero(t, state.Subscriptions[0].AmountUsed)
			require.Zero(t, state.Subscriptions[2].AmountUsed)
		}
		f.fake.mu.Lock()
		state.Outbound = append(state.Outbound, f.fake.calls...)
		f.fake.mu.Unlock()
		paths := []string{}
		for _, call := range state.Outbound {
			u, err := url.Parse(call.URL)
			require.NoError(t, err)
			paths = append(paths, u.Path)
		}
		expected := append([]string{}, tc.Calls...)
		if tc.Group == "pay" && tc.Provider == "pancake" {
			slices.Sort(paths)
			slices.Sort(expected)
		}
		require.Equal(t, expected, paths, "outbound call count/order")
		replacements := map[string]string{}
		for i := range state.Orders {
			row := &state.Orders[i]
			if tc.Group == "pay" || tc.Group == "balance" {
				require.NotEmpty(t, row.TradeNo)
				replacements[row.TradeNo] = "<generated-order>"
				row.TradeNo = "<generated-order>"
			}
			if row.CreateTime >= started-5 {
				replacements[`"create_time":`+strconv.FormatInt(row.CreateTime, 10)] = `"create_time":1`
				row.CreateTime = 1
			}
			if row.CompleteTime >= started-5 {
				replacements[`"complete_time":`+strconv.FormatInt(row.CompleteTime, 10)] = `"complete_time":1`
				row.CompleteTime = 1
			}
		}
		for i := range state.Plans {
			row := &state.Plans[i]
			for _, v := range []int64{row.CreatedAt, row.UpdatedAt} {
				if v >= started-5 {
					replacements[`"created_at":`+strconv.FormatInt(v, 10)] = `"created_at":1`
					replacements[`"updated_at":`+strconv.FormatInt(v, 10)] = `"updated_at":1`
				}
			}
			row.CreatedAt = 1
			row.UpdatedAt = 1
		}
		for i := range state.Subscriptions {
			row := &state.Subscriptions[i]
			row.CreatedAt = 1
			row.UpdatedAt = 1
			if row.EndTime >= started-5 && row.EndTime <= time.Now().Unix()+5 {
				replacements[`"end_time":`+strconv.FormatInt(row.EndTime, 10)] = `"end_time":1`
				row.EndTime = 1
			}
		}
		for i := range state.History {
			if state.History[i].CompleteTime > 100 {
				state.History[i].CompleteTime = 1
			}
		}
		for i := range state.Logs {
			row := &state.Logs[i]
			row.CreatedAt = 1
			row.RequestId = fmt.Sprintf("<usage-%d>", i+1)
		}
		for i := range state.Events {
			row := &state.Events[i]
			row.CreatedAt = 1
			row.EventId = fmt.Sprintf("<event-%d>", i+1)
			if replacement, ok := requestIDs[row.RequestId]; ok {
				row.RequestId = replacement
			} else {
				require.NotEmpty(t, row.RequestId)
				row.RequestId = "<request-1>"
			}
		}
		for old, value := range replacements {
			state.HTTP.Body = strings.ReplaceAll(state.HTTP.Body, old, value)
			for i := range state.Outbound {
				state.Outbound[i].Body = strings.ReplaceAll(state.Outbound[i].Body, old, value)
			}
		}
		for i := range state.Outbound {
			state.Outbound[i].Body = normalizeOutgoing(t, state.Outbound[i], tc)
		}
		if tc.Provider == "pancake" && tc.Group == "pay" {
			slices.SortFunc(state.Outbound, func(a, b outbound) int { return strings.Compare(a.URL, b.URL) })
		}
		state.HTTP.Body = redactFixtureResponse(t, state.HTTP.Body, tc)
		encoded, err := common.Marshal(state)
		require.NoError(t, err)
		for _, secret := range []string{testPrivateKey, setting.StripeApiSecret, setting.StripeWebhookSecret, setting.CreemApiKey, setting.CreemWebhookSecret, operation_setting.EpayKey, "test-only-checkout-token"} {
			if secret != "" {
				require.NotContains(t, string(encoded), secret)
			}
		}
		var count int64
		require.NoError(t, f.env.DB.Model(&model.Log{}).Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, f.env.DB.Model(&model.AuditLog{}).Count(&count).Error)
		require.EqualValues(t, 1, count)
		steps = append(steps, state)
	}
	data, err := common.Marshal(steps)
	require.NoError(t, err)
	if *preflight {
		if dir := os.Getenv("SUBSCRIPTIONS_CONTRACT_STATE_EVIDENCE"); dir != "" {
			require.NoError(t, os.MkdirAll(dir, 0o700))
			file, err := os.OpenFile(filepath.Join(dir, tc.Name+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			require.NoError(t, err)
			_, err = file.Write(append(data, '\n'))
			require.NoError(t, err)
			require.NoError(t, file.Close())
		}
		httpData, err := common.Marshal(steps[0].HTTP)
		require.NoError(t, err)
		fmt.Printf("SUBSCRIPTIONS_HTTP %s %s\n", tc.Name, base64.StdEncoding.EncodeToString(httpData))
		fmt.Printf("SUBSCRIPTIONS_SNAPSHOT %s %x\n", tc.Name, sha256.Sum256(data))
		return
	}
	contracttest.GoldenValue(t, "subscriptions_"+tc.Name, steps)
}

func runChild(t *testing.T, name, language string, updating, checking bool) []byte {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestSubscriptionsContract$", "-test.count=1", "-test.timeout=90s", "-update="+strconv.FormatBool(updating), "-subscriptions-preflight="+strconv.FormatBool(checking))
	cmd.Env = append(os.Environ(), "SUBSCRIPTIONS_CONTRACT_CASE="+name, "SUBSCRIPTIONS_CONTRACT_LANGUAGE="+language, "TZ=UTC")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "case %s: %s", name, output)
	return output
}

// Recording can only modify a verified disposable archive of the pinned origin.
func verifyOriginRecording(t *testing.T) {
	t.Helper()
	require.False(t, *manifest || *preflight)
	require.Equal(t, originCommit, os.Getenv("SUBSCRIPTIONS_CONTRACT_ORIGIN_RECORD"))
	root, err := filepath.EvalSymlinks(os.Getenv("SUBSCRIPTIONS_CONTRACT_ORIGIN_DIR"))
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "internal/subscriptions/contract"), cwd)
	for dir := root; ; dir = filepath.Dir(dir) {
		marker := filepath.Join(dir, ".git")
		info, err := os.Stat(marker)
		if !os.IsNotExist(err) {
			require.NoError(t, err)
			// Empty read-only placeholders at managed writable roots are not Git metadata.
			require.Contains(t, []string{"/tmp", "/dev/shm"}, dir)
			require.True(t, info.IsDir())
			require.Zero(t, info.Mode().Perm()&0o222)
			entries, err := os.ReadDir(marker)
			require.NoError(t, err)
			require.Empty(t, entries, "recording inside a checkout is forbidden")
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	marker, err := os.ReadFile(filepath.Join(root, ".subscriptions-origin"))
	require.NoError(t, err)
	require.Equal(t, originCommit+"\n", string(marker))
	data, err := os.ReadFile(filepath.Join(root, ".subscriptions-source.sha256"))
	require.NoError(t, err)
	var hashes map[string]string
	require.NoError(t, common.Unmarshal(data, &hashes))
	require.NotEmpty(t, hashes)
	for name, digest := range hashes {
		require.False(t, filepath.IsAbs(name) || strings.Contains(name, ".."))
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(data)), name)
	}
	require.NoFileExists(t, filepath.Join(root, "server/go.mod"))
}

type fixture struct {
	env             *contracttest.Env
	log             *gorm.DB
	fake            *providerTransport
	bearers         map[string]string
	key             *rsa.PrivateKey
	trade           string
	commitRollbacks int
}

func newFixture(t *testing.T, tc contractCase) *fixture {
	t.Helper()
	require.NoError(t, sqliteclock.RegisterScalarFunction("strftime", 2, func(_ *sqliteclock.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0] != "%s" || args[1] != "now" {
			return nil, fmt.Errorf("unexpected DB clock query")
		}
		return strconv.FormatInt(dbEpoch, 10), nil
	}))
	env := contracttest.New(t)
	constant.AnonymousRequestBodyLimitKB = 512
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.ItemsPerPage = 10
	common.QuotaPerUnit = 500000
	common.NodeName = "subscriptions-contract-node"
	common.Version = "contract"
	common.SystemName = "New API"
	operation_setting.GetGeneralSetting().QuotaDisplayType = "USD"
	operation_setting.GetGeneralSetting().CustomCurrencySymbol = "C"
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 2
	*operation_setting.GetPaymentSetting() = operation_setting.PaymentSetting{ComplianceConfirmed: true, ComplianceTermsVersion: operation_setting.CurrentComplianceTermsVersion, AmountOptions: []int{10, 20}, AmountDiscount: map[int]float64{}}
	operation_setting.PayAddress = "https://epay.example.test"
	operation_setting.EpayId = "test-merchant"
	operation_setting.EpayKey = "epay-test-only-secret"
	operation_setting.PayMethods = []map[string]string{{"name": "Alipay", "type": "alipay"}}
	operation_setting.Price = 1
	operation_setting.MinTopUp = 1
	operation_setting.CustomCallbackAddress = ""
	operation_setting.USDExchangeRate = 7
	system_setting.ServerAddress = "https://console.example.test/"
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	block, _ := pem.Decode([]byte(testPrivateKey))
	require.NotNil(t, block)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	key := parsed.(*rsa.PrivateKey)
	publicBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes}))
	setting.StripeApiSecret = "sk_test_fixture"
	setting.StripeWebhookSecret = "whsec_fixture"
	setting.StripePriceId = "price_fixture"
	setting.StripeUnitPrice = 1
	setting.StripeMinTopUp = 1
	setting.StripePromotionCodesEnabled = false
	setting.CreemApiKey = "creem-test-only-secret"
	setting.CreemWebhookSecret = "creem-test-webhook-secret"
	setting.CreemProducts = `[{"productId":"prod_test","name":"Fixture credits","price":12.34,"currency":"USD","quota":12345}]`
	setting.CreemTestMode = false
	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = "waffo-test-only-secret"
	setting.WaffoPrivateKey = base64.StdEncoding.EncodeToString(block.Bytes)
	setting.WaffoPublicCert = base64.StdEncoding.EncodeToString(publicBytes)
	setting.WaffoSandboxApiKey = setting.WaffoApiKey
	setting.WaffoSandboxPrivateKey = setting.WaffoPrivateKey
	setting.WaffoSandboxPublicCert = setting.WaffoPublicCert
	setting.WaffoMerchantId = "MER_1234567890abcdefghijkl"
	setting.WaffoCurrency = "USD"
	setting.WaffoUnitPrice = 1
	setting.WaffoMinTopUp = 1
	setting.WaffoNotifyUrl = ""
	setting.WaffoReturnUrl = ""
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMap["WaffoPayMethods"] = `[{"payMethodType":"CARD","payMethodName":"VISA"}]`
	common.OptionMapRWMutex.Unlock()
	setting.WaffoPancakeMerchantID = "MER_1234567890abcdefghijkl"
	setting.WaffoPancakePrivateKey = testPrivateKey
	setting.WaffoPancakeStoreID = storeID
	setting.WaffoPancakeProductID = productID
	setting.WaffoPancakeReturnURL = ""
	setting.WaffoPancakeUnitPrice = 1
	setting.WaffoPancakeMinTopUp = 1
	t.Setenv("WAFFO_WEBHOOK_TEST_PUBLIC_KEY", public)
	t.Setenv("WAFFO_WEBHOOK_PROD_PUBLIC_KEY", public)
	t.Setenv("WAFFO_WEBHOOK_PUBLIC_KEY", "")
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "payments-log.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	previousLog := model.LOG_DB
	model.LOG_DB = logDB
	t.Cleanup(func() {
		model.LOG_DB = previousLog
		db, err := logDB.DB()
		if assert.NoError(t, err) {
			assert.NoError(t, db.Close())
		}
	})
	require.NoError(t, env.DB.Create(&model.Log{Id: 9001, UserId: 1, Type: 1, Content: "main-sentinel", RequestId: "main-sentinel"}).Error)
	require.NoError(t, env.DB.Create(&model.AuditLog{Id: 9001, EventId: "main-sentinel", UserId: 1, ActorRole: 1, CreatedAt: 100, Category: "security", RequestId: "main-sentinel"}).Error)
	f := &fixture{env: env, log: logDB, key: key, trade: testTrade, bearers: map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root, "user_pat": env.UserPAT, "admin_pat": "subscriptions-admin-pat", "root_pat": "subscriptions-root-pat", "api_key": "sk-subscriptions-gateway-key"}}
	for id, pat := range map[int]string{2: f.bearers["admin_pat"], 3: f.bearers["root_pat"]} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", pat).Error)
	}
	require.NoError(t, env.DB.Create(&model.Token{Id: 101, UserId: 1, Key: "subscriptions-gateway-key", Status: 1, ExpiredTime: -1, UnlimitedQuota: true, Name: "gateway-key", Group: "default"}).Error)
	require.NoError(t, env.DB.Model(&model.User{}).Where("id IN ?", []int{1, 2, 3}).Updates(map[string]any{"email": "", "stripe_customer": "", "setting": `{}`}).Error)
	i18n.SetUserLangLoader(model.GetUserLanguage)
	f.fake = &providerTransport{t: t, tc: tc, key: key, calls: []outbound{}, checkoutReady: make(chan struct{})}
	previousTransport, previousClient := http.DefaultTransport, http.DefaultClient
	http.DefaultTransport = f.fake
	http.DefaultClient = &http.Client{Transport: f.fake}
	previousStripe := stripe.GetBackend(stripe.APIBackend)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: &http.Client{Transport: f.fake}}))
	t.Cleanup(func() {
		http.DefaultTransport = previousTransport
		http.DefaultClient = previousClient
		stripe.SetBackend(stripe.APIBackend, previousStripe)
	})
	configureCase(t, f, tc)
	return f
}

type outbound struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type providerTransport struct {
	t                *testing.T
	tc               contractCase
	key              *rsa.PrivateKey
	mu               sync.Mutex
	calls            []outbound
	checkoutReady    chan struct{}
	checkoutArrivals int
}

// RoundTrip captures requests after SDK signing; all unrecognized destinations fail closed.
func (p *providerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	for _, key := range []string{"Content-Type", "Authorization", "X-API-KEY", "X-SIGNATURE", "X-Merchant-Id", "X-Timestamp", "X-Idempotency-Key", "Idempotency-Key", "X-API-VERSION", "X-SDK-VERSION", "Stripe-Version", "User-Agent", "X-Stripe-Client-User-Agent", "X-Stripe-Client-Telemetry", "Stripe-Account"} {
		value := r.Header.Get(key)
		if value == "" {
			continue
		}
		switch key {
		case "Authorization", "X-API-KEY":
			headers[key] = "<fixture-credential>"
		case "X-SIGNATURE":
			headers[key] = "<verified-signature>"
		case "X-Timestamp":
			headers[key] = "<timestamp>"
		case "X-Stripe-Client-User-Agent":
			headers[key] = contracttest.NormalizeStripeClientUserAgent(p.t, value)
		case "X-Idempotency-Key", "Idempotency-Key":
			headers[key] = "<verified-idempotency>"
		default:
			headers[key] = value
		}
	}
	assert.Equal(p.t, "POST", r.Method)
	path := r.URL.Path
	switch r.URL.Host {
	case "api.stripe.com":
		assert.Equal(p.t, "Bearer sk_test_fixture", r.Header.Get("Authorization"))
		assert.Equal(p.t, "/v1/checkout/sessions", path)
		values, err := url.ParseQuery(string(data))
		require.NoError(p.t, err)
		assert.Equal(p.t, "subscription", values.Get("mode"))
		assert.Equal(p.t, "price_subscription", values.Get("line_items[0][price]"))
		assert.Equal(p.t, "1", values.Get("line_items[0][quantity]"))
		assert.Equal(p.t, "https://console.example.test/wallet", values.Get("success_url"))
		assert.Equal(p.t, values.Get("success_url"), values.Get("cancel_url"))
		assert.False(p.t, values.Has("allow_promotion_codes"))
	case "api.creem.io", "test-api.creem.io":
		assert.Equal(p.t, setting.CreemApiKey, r.Header.Get("x-api-key"))
		assert.Equal(p.t, "/v1/checkouts", path)
		var body struct {
			Product  string            `json:"product_id"`
			Metadata map[string]string `json:"metadata"`
		}
		require.NoError(p.t, common.Unmarshal(data, &body))
		assert.Equal(p.t, "prod_subscription", body.Product)
		assert.Equal(p.t, "0", body.Metadata["quota"])
		assert.Equal(p.t, "Contract plan", body.Metadata["product_name"])
	case "api.waffo.com", "api-sandbox.waffo.com":
		assert.True(p.t, waffoutils.Verify(string(data), r.Header.Get("X-SIGNATURE"), setting.WaffoPublicCert), "outgoing Waffo signature")
		assert.Equal(p.t, setting.WaffoApiKey, r.Header.Get("X-API-KEY"))
		assert.Equal(p.t, "/api/v1/order/create", path)
	case "api.waffo.ai":
		bodyHash := sha256.Sum256(data)
		canonical := r.Method + "\n" + path + "\n" + r.Header.Get("X-Timestamp") + "\n" + base64.StdEncoding.EncodeToString(bodyHash[:])
		signature, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Signature"))
		assert.NoError(p.t, err)
		digest := sha256.Sum256([]byte(canonical))
		assert.NoError(p.t, rsa.VerifyPKCS1v15(&p.key.PublicKey, crypto.SHA256, digest[:], signature))
		assert.Equal(p.t, "MER_1234567890abcdefghijkl", r.Header.Get("X-Merchant-Id"))
		if path == "/v1/graphql" {
			assert.Empty(p.t, r.Header.Get("X-Idempotency-Key"))
		} else {
			ts, err := strconv.ParseInt(r.Header.Get("X-Timestamp"), 10, 64)
			assert.NoError(p.t, err)
			input := "MER_1234567890abcdefghijkl:" + path + ":" + string(data)
			if strings.Contains(path, "checkout/") || strings.Contains(path, "issue-session-token") {
				input = fmt.Sprintf("%s:%d", input, ts/60)
			}
			hash := sha256.Sum256([]byte(input))
			assert.Equal(p.t, hex.EncodeToString(hash[:]), r.Header.Get("X-Idempotency-Key"))
		}
	default:
		assert.Fail(p.t, "unexpected provider destination", r.URL.Host)
		return nil, fmt.Errorf("unexpected provider destination %s", r.URL.Host)
	}
	p.mu.Lock()
	p.calls = append(p.calls, outbound{r.Method, r.URL.String(), headers, string(data)})
	concurrent := p.tc.Provider == "pancake" && p.tc.Group == "pay"
	if concurrent {
		p.checkoutArrivals++
		if p.checkoutArrivals == 2 {
			close(p.checkoutReady)
		}
	}
	p.mu.Unlock()
	if concurrent {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		select {
		case <-p.checkoutReady:
		case <-ctx.Done():
			assert.Fail(p.t, "authenticated checkout must launch both requests")
			return nil, ctx.Err()
		}
	}

	body := `{}`
	status := 200
	switch r.URL.Host {
	case "api.stripe.com":
		body = `{"id":"cs_fixture","object":"checkout.session","url":"https://checkout.example.test/stripe"}`
	case "api.creem.io", "test-api.creem.io":
		body = `{"id":"checkout_fixture","checkout_url":"https://checkout.example.test/creem"}`
	case "api.waffo.com", "api-sandbox.waffo.com":
		body = `{"code":"0","data":{"orderAction":"https://checkout.example.test/waffo"}}`
	case "api.waffo.ai":
		switch path {
		case "/v1/actions/auth/issue-session-token":
			body = `{"data":{"token":"test-only-checkout-token","expiresAt":"2099-01-01T00:00:00Z"}}`
		case "/v1/actions/checkout/create-session":
			body = `{"data":{"sessionId":"session_fixture","checkoutUrl":"https://checkout.example.test/pancake","expiresAt":"2099-01-01T00:00:00Z"}}`
		case "/v1/actions/store/create-store":
			body = `{"data":{"store":{"id":"STO_1234567890abcdefghijkl","name":"new-api-store"}}}`
		case "/v1/actions/onetime-product/create-product", "/v1/actions/onetime-product/publish-product":
			body = `{"data":{"product":{"id":"PROD_1234567890abcdefghijkl","name":"new-api-charge-product"}}}`
		case "/v1/graphql":
			body = `{"data":{"stores":[{"id":"STO_1234567890abcdefghijkl","name":"Fixture store","status":"active","prodEnabled":true,"onetimeProducts":[{"id":"PROD_1234567890abcdefghijkl","name":"Fixture product","status":"active"}]}]}}`
		default:
			assert.Fail(p.t, "unexpected Pancake path", path)
			return nil, fmt.Errorf("unexpected Pancake path %s", path)
		}
	}
	failed := p.tc.Setup == "upstream_error" || (p.tc.Setup == "orphan_product" && strings.Contains(path, "create-product")) || (p.tc.Setup == "orphan_publish" && strings.Contains(path, "publish-product"))
	if failed {
		status = 400
		body = `{"error":{"type":"invalid_request_error","message":"fixture provider failure"},"errors":[{"message":"fixture provider failure"}],"code":"E0001"}`
	}
	if p.tc.Setup == "upstream_malformed" {
		body = "{"
	}
	if p.tc.Setup == "upstream_empty" && !strings.Contains(path, "issue-session-token") {
		switch p.tc.Provider {
		case "stripe":
			body = `{"id":"cs_fixture","object":"checkout.session"}`
		case "creem":
			body = `{"id":"checkout_fixture"}`
		case "waffo":
			body = `{"code":"0","data":{}}`
		case "pancake":
			body = `{"data":{}}`
		}
	}
	if p.tc.Setup == "empty_catalog" && p.tc.Group == "products" {
		body = `{"data":{"stores":[]}}`
	}
	if p.tc.Setup == "inactive_products" && p.tc.Group == "products" {
		body = `{"data":{"stores":[{"id":"STO_1234567890abcdefghijkl","name":"Fixture store","status":"active","prodEnabled":true,"onetimeProducts":[{"id":"PROD_1234567890abcdefghijkl","name":"Kept","status":" ACTIVE "},{"id":"PROD_inactive123456","name":"Dropped","status":"draft"}]}]}}`
	}
	response := &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
	response.Header.Set("Content-Type", "application/json")
	if p.tc.Provider == "waffo" {
		sig, err := waffoutils.Sign(body, setting.WaffoPrivateKey)
		assert.NoError(p.t, err)
		if p.tc.Setup == "bad_response_signature" {
			sig = "invalid"
		}
		response.Header.Set("X-SIGNATURE", sig)
	}
	return response, nil
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("fixture body read failure") }

func (failingBody) Close() error { return nil }

func webhookRequest(t *testing.T, f *fixture, tc contractCase, step int) *http.Request {
	t.Helper()
	event := tc.Event
	if step > 0 && tc.Next != "" {
		event = tc.Next
	}
	body := ""
	header := ""
	sig := ""
	switch tc.Provider {
	case "stripe":
		kind := "checkout.session.completed"
		status := "complete"
		paid := "paid"
		switch event {
		case "unknown":
			kind = "customer.created"
		case "unpaid", "completed_unpaid":
			paid = "unpaid"
		case "completed_wrong_status":
			status = "open"
		case "async_success":
			kind = "checkout.session.async_payment_succeeded"
		case "async_failed", "failure_then_paid":
			kind = "checkout.session.async_payment_failed"
		case "expired":
			kind = "checkout.session.expired"
			status = "expired"
		case "expired_wrong_status":
			kind = "checkout.session.expired"
			status = "open"
		}
		amount, currency := "350", "usd"
		if tc.Setup == "wrong_money" {
			amount, currency = "1", "eur"
		}
		body = fmt.Sprintf(`{"id":"evt_fixture","object":"event","type":%q,"data":{"object":{"id":"cs_fixture","object":"checkout.session","customer":"cus_fixture","client_reference_id":%q,"status":%q,"payment_status":%q,"amount_total":%s,"currency":%q}}}`, kind, f.trade, status, paid, amount, currency)
		if tc.Setup == "malformed_event" {
			body = "{"
		}
		ts := time.Now().Unix()
		if tc.Setup == "stale_signature" {
			ts -= 3600
		}
		h := hmac.New(sha256.New, []byte(setting.StripeWebhookSecret))
		fmt.Fprintf(h, "%d.%s", ts, body)
		sig = fmt.Sprintf("t=%d,v1=%x", ts, h.Sum(nil))
		header = "Stripe-Signature"
	case "creem":
		kind, status := "checkout.completed", "paid"
		if event == "unknown" {
			kind = "checkout.created"
		}
		if event == "unpaid" {
			status = "pending"
		}
		amount, currency := 350, "USD"
		if tc.Setup == "wrong_money" {
			amount, currency = 1, "EUR"
		}
		body = fmt.Sprintf(`{"id":"evt_fixture","eventType":%q,"object":{"request_id":%q,"order":{"id":"order_fixture","status":%q,"type":"onetime","amount_paid":%d,"currency":%q},"customer":{"email":"buyer@example.test","name":"Fixture Buyer"},"product":{"name":"Fixture product"}}}`, kind, f.trade, status, amount, currency)
		if tc.Setup == "malformed_event" {
			body = "{"
		}
		if tc.Setup == "not_onetime" {
			body = strings.ReplaceAll(body, `"type":"onetime"`, `"type":"recurring"`)
		}
		if tc.Setup == "missing_request" {
			body = strings.ReplaceAll(body, f.trade, "")
		}
		if tc.Setup == "empty_email" {
			body = strings.ReplaceAll(body, "buyer@example.test", "")
		}
		if tc.Setup == "data_type" {
			body = `{"eventType":"checkout.completed","object":"invalid"}`
		}
		h := hmac.New(sha256.New, []byte(setting.CreemWebhookSecret))
		h.Write([]byte(body))
		sig = hex.EncodeToString(h.Sum(nil))
		header = "creem-signature"
	case "waffo":
		kind, status := "PAYMENT_NOTIFICATION", "PAY_SUCCESS"
		if event == "unknown" {
			kind = "SUBSCRIPTION_STATUS_NOTIFICATION"
		}
		if event == "unpaid" {
			status = "PAY_IN_PROGRESS"
		}
		money, currency := "3.50", "USD"
		if tc.Setup == "wrong_money" {
			money, currency = "0.01", "EUR"
		}
		body = fmt.Sprintf(`{"eventType":%q,"result":{"merchantOrderId":%q,"orderStatus":%q,"orderAmount":%q,"orderCurrency":%q}}`, kind, f.trade, status, money, currency)
		if tc.Setup == "malformed_event" {
			body = "{"
		}
		var err error
		sig, err = waffoutils.Sign(body, setting.WaffoPrivateKey)
		require.NoError(t, err)
		header = "X-SIGNATURE"
	case "pancake":
		kind, mode, identity, external := "order.completed", "test", "new-api-user-1", f.trade
		if tc.Setup == "prod" {
			mode = "prod"
		}
		if tc.Setup == "mode_case" {
			mode = " TEST "
		}
		if event == "unknown" {
			kind = "order.created"
		}
		if tc.Setup == "wrong_env" {
			mode = "prod"
		}
		if tc.Setup == "buyer_mismatch" {
			identity = "new-api-user-2"
		}
		if tc.Setup == "external_missing" {
			external = ""
		}
		money, currency := "3.50", "USD"
		if tc.Setup == "wrong_money" {
			money, currency = "0.01", "EUR"
		}
		body = fmt.Sprintf(`{"id":"evt_fixture","eventType":%q,"eventId":"event_fixture","storeId":"STO_1234567890abcdefghijkl","mode":%q,"data":{"orderId":"ORD_internal123456","orderMerchantExternalId":%q,"merchantProvidedBuyerIdentity":%q,"amount":%q,"currency":%q}}`, kind, mode, external, identity, money, currency)
		if tc.Setup == "malformed_event" {
			body = "{"
		}
		if tc.Setup == "data_type" {
			body = `{"mode":"test","eventType":"order.completed","data":"invalid"}`
		}
		ts := time.Now().UnixMilli()
		if tc.Setup == "stale_signature" {
			ts -= 3600000
		}
		digest := sha256.Sum256([]byte(strconv.FormatInt(ts, 10) + "." + body))
		signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		sig = fmt.Sprintf("t=%d,v1=%s", ts, base64.StdEncoding.EncodeToString(signature))
		header = "X-Waffo-Signature"
	case "epay":
		status := "TRADE_SUCCESS"
		if event == "unknown" || event == "unpaid" {
			status = "WAIT_BUYER_PAY"
		}
		method := "alipay"
		if tc.Setup == "method_switch" {
			method = "wxpay"
		}
		money, pid := "3.50", operation_setting.EpayId
		if tc.Setup == "wrong_money" {
			money, pid = "0.01", "different-merchant"
		}
		params := map[string]string{"pid": pid, "type": method, "out_trade_no": f.trade, "trade_no": "epay_fixture", "trade_status": status, "money": money, "sign_type": "MD5"}
		params = epay.GenerateParams(params, operation_setting.EpayKey)
		if tc.Setup == "bad_signature" || tc.Setup == "get_bad_signature" {
			params["sign"] = "invalid"
		}
		if tc.Setup == "missing_signature" {
			delete(params, "sign")
		}
		if tc.Setup == "malformed_event" {
			params = map[string]string{}
		}
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		path := tc.Path
		if tc.Method == "GET" {
			path += "?" + values.Encode()
		}
		if tc.Setup == "post_query_ignored" {
			path += "?sign=invalid&out_trade_no=foreign"
		}
		r := httptest.NewRequest(tc.Method, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if tc.Setup == "read_failure" {
			r.Body = failingBody{}
		}
		return r
	}
	if tc.Setup == "bad_signature" {
		sig = "invalid"
	}
	if tc.Setup == "missing_signature" {
		sig = ""
	}
	r := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(header, sig)
	if tc.Setup == "read_failure" {
		r.Body = failingBody{}
	}
	return r
}

// Only generated references/times are normalized; all financial and provider body fields stay exact.
func normalizeOutgoing(t *testing.T, call outbound, tc contractCase) string {
	t.Helper()
	body := call.Body
	if tc.Provider == "stripe" {
		values, err := url.ParseQuery(body)
		require.NoError(t, err)
		ref := values.Get("client_reference_id")
		require.NotEmpty(t, ref)
		body = strings.ReplaceAll(body, ref, "<generated-order>")
	}
	if tc.Provider == "creem" || tc.Provider == "waffo" || tc.Provider == "pancake" {
		var object map[string]json.RawMessage
		require.NoError(t, common.UnmarshalJsonStr(body, &object))
		for _, name := range []string{"request_id", "merchantOrderId", "paymentRequestId", "orderMerchantExternalId", "orderRequestedAt"} {
			raw := object[name]
			if raw == nil {
				continue
			}
			var value string
			require.NoError(t, common.Unmarshal(raw, &value))
			if strings.Contains(name, "RequestedAt") {
				_, err := time.Parse("2006-01-02T15:04:05.000Z", value)
				require.NoError(t, err)
				body = strings.ReplaceAll(body, value, "<request-time>")
			} else {
				require.NotEmpty(t, value)
				body = strings.ReplaceAll(body, value, "<generated-order>")
			}
		}
	}
	return body
}

// Synthetic links/tokens/signatures are checked before redaction and never stored as usable credentials.
func redactFixtureResponse(t *testing.T, body string, tc contractCase) string {
	t.Helper()
	if tc.Provider == "epay" && tc.Group == "pay" && tc.Outcome == "success" {
		var response struct {
			Data map[string]string `json:"data"`
		}
		require.NoError(t, common.UnmarshalJsonStr(body, &response))
		signature := response.Data["sign"]
		require.NotEmpty(t, signature)
		body = strings.ReplaceAll(body, signature, "<verified-signature>")
	}
	for _, value := range []string{"https://checkout.example.test/stripe", "https://checkout.example.test/creem", "https://checkout.example.test/waffo", "https://checkout.example.test/pancake#token=test-only-checkout-token"} {
		if strings.Contains(body, value) {
			body = strings.ReplaceAll(body, value, "<fixture-checkout-link>")
		}
	}
	return strings.ReplaceAll(body, "test-only-checkout-token", "<fixture-token>")
}

func writeLocaleEvidence(t *testing.T, destination string) {
	t.Helper()
	evidence := map[string]httpSnapshot{}
	for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "xx-YY"} {
		for _, name := range localeCases() {
			output := runChild(t, name, language, false, true)
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "SUBSCRIPTIONS_HTTP ") {
					data, err := base64.StdEncoding.DecodeString(strings.Fields(line)[2])
					require.NoError(t, err)
					var snapshot httpSnapshot
					require.NoError(t, common.Unmarshal(data, &snapshot))
					evidence[language+" "+name] = snapshot
					found = true
				}
			}
			require.True(t, found, name)
		}
	}
	data, err := common.Marshal(evidence)
	require.NoError(t, err)
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.Write(append(data, '\n'))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// This RSA key is public test fixture material and must never be used outside tests.
const testPrivateKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDWKMfvtNOc5oi5
6ZOX0v28K+imcAilK70mUPhYpQrjY1JudnA/02GM+NuRn2Bj5C3tt7YmEAwd88ii
SQ8fpIN7JXRXJz2JGM4V5jcpho5A7bpb3JjMLG+4gXZBA9k7WHLuQPmDTvR3TN6p
wZzUv0V6K5LiGM8KWKeTeBIIRZO+ywJ2Ju/8z/AB5WpYwEO4Acws075JTHYSXOWM
QLuuKzxUKsr+h9xHN/jBoKVszpXESn1BtoZlRHBBUHJyCfIjkJYznM9vm9/Gyrmt
ClI9fBC/vjKExu6h9DKXIpoRShRkSUFMzlP9E0/3owsKoXV7hDWZCrGLB3GFl4DE
svQ9vJu5AgMBAAECggEABv8yTxEgLzqFYdcDig7+WHSNgt0kHOGojn4ZPVmcfaLg
oGY1kyRDGFQHQUxDwnoD1SF35I4bBYbIy/37tx4M1cwfStbxC2s88VBvz3BUltVC
Fwzh1q9LH5CdQH6/KJGHCcARn/6r6xvDgd8d0l1ytehwnQ1ihN5EbDFBUv7KFtd2
CaLK5t9CjrTokFzaiw0UR0HFyxPNtw/sKg5AVTMwwVcPodn3eQ5QtSlgogi1gPcy
6K0umXRmI65P6dl1Z59eAfnFy/tbSs3yU7NdSspAYb+3qfjb8knCUVl7wInReQrY
HJmFT0522RFuFH9QCj4O9B46TDZOkjsKAGvmlEZQjQKBgQDu7E9LcxgzbOUo4veA
4NSMQo/asDRRy+WsgBlNrzqXQwy285l8dSAHh8+B6OkM054qUSbNflOe0445ZjVc
jD5iDKuRyDuAHCKPV2z7yLtrBIxjVyT/Y5RGa0og0CaXBi0fJMONYDser78npGoZ
zXUHMdHV8x8937eOCqEq97PLjQKBgQDld1s+hleWs9WlpixduJFHKwfpJl+FzjkK
njPpVAI4o5rpPw1I2X5ACTODtBtbsPhhHSwS/dYxHUq2BMdhhpNlPwNWtsbx7e6p
DP9MAp5SUh57F4UJS2HYHWcO++v/5JX9WwpC7dkXk2S6CSw2lGom2qT683TVuXyQ
1T85LIMv3QKBgFQY4aBRFsejINZPbYbznFzyYOg89W2FM4aQYFezwg/UDfmnrPLd
1zRPXX8AmGdnZVjHz7ytJonVNU6G52l4A81HYGyj6i2huZhy0W2erjlxFGkzF1a9
mkdfJGyAV9T425EGw75TjPu0KLivaplCdFrf8lg4h1qkEFvy6d/R6HtRAoGBAM05
G0TnN6zTt49H+EP1zxCcXp3ZsPLlxkRFy12xwa7QO7rBpmryAaTZ1dXDnfg6ZVnN
kl6F4SvZ67iwu3TwvCB/48eI/pLe3mUY97cOXHR5d0NVfKkcXyy+1y4ihyY8VRzb
cGj9efxI2Xw8uesyf2GyiHzcgjCnjM2nuryyn72NAoGALWQ5GMGIPRw5WrOR9wUn
0/UI1M+1LPl7sG6xkAjhrnfqJVOsPG/0yd3q/cwD7BzfcpADJp+b4oJZ19Y9Ew2N
mfraIfNUwAottfUNDtlLKHKpezyy/OrAUJJkRR4fZcIVcbsIHRZ0w5wztsZWOTta
gKZndZIXnD9QeYFHwHEw+a4=
-----END PRIVATE KEY-----
`

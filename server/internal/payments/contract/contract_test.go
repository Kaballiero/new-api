// Package contract pins payment HTTP, database and outgoing provider contracts before migration.
package contract

import (
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"

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
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	waffonet "github.com/waffo-com/waffo-go/net"
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
const testTrade = "PAYMENT-CONTRACT-ORDER"
const storeID = "STO_1234567890abcdefghijkl"
const productID = "PROD_1234567890abcdefghijkl"

var manifest = flag.Bool("payments-manifest", false, "print the case manifest without IO")
var preflight = flag.Bool("payments-preflight", false, "assert HTTP/state/outbound without goldens")

// Expectations are declared before execution; recording cannot turn a skipped effect into success.
type contractCase struct {
	Name, Group, Provider, Actor, Method, Path, Body, Setup, Event string
	Status                                                         int
	Outcome                                                        string
	Credit                                                         int
	Logs                                                           int
	Orders                                                         int
	OrderStatus                                                    string
	Calls                                                          []string
	Repeat                                                         bool
	Next                                                           string
	Subscription                                                   bool
	Total                                                          int
	IDs                                                            []int
	Credits                                                        []int
	LogCounts                                                      []int
	Statuses                                                       []string
	Outcomes                                                       []string
}

func routes() []contractCase {
	return []contractCase{
		{Name: "info", Group: "info", Actor: "user", Method: "GET", Path: "/api/user/topup/info", Outcome: "api", Status: 200},
		{Name: "self", Group: "self", Actor: "user", Method: "GET", Path: "/api/user/topup/self", Outcome: "api", Status: 200},
		{Name: "admin_list", Group: "admin_list", Actor: "admin", Method: "GET", Path: "/api/user/topup", Outcome: "api", Status: 200},
		{Name: "manual", Group: "manual", Provider: "epay", Actor: "admin", Method: "POST", Path: "/api/user/topup/complete", Body: `{"trade_no":"PAYMENT-CONTRACT-ORDER"}`, Outcome: "api", Status: 200, Orders: 1, OrderStatus: "success", Credit: 1000000, Logs: 1},
		{Name: "compliance", Group: "compliance", Actor: "root", Method: "POST", Path: "/api/option/payment_compliance", Body: `{"confirmed":true}`, Outcome: "api", Status: 200},
		{Name: "catalog", Group: "catalog", Provider: "pancake", Actor: "root", Method: "GET", Path: "/api/option/waffo-pancake/catalog", Outcome: "success", Status: 200, Calls: []string{"/v1/graphql"}},
		{Name: "pair", Group: "pair", Provider: "pancake", Actor: "root", Method: "POST", Path: "/api/option/waffo-pancake/pair", Body: `{}`, Outcome: "success", Status: 200, Calls: []string{"/v1/actions/store/create-store", "/v1/actions/onetime-product/create-product", "/v1/actions/onetime-product/publish-product"}},
		{Name: "save", Group: "save", Provider: "pancake", Actor: "root", Method: "POST", Path: "/api/option/waffo-pancake/save", Body: `{"merchant_id":"MER_1234567890abcdefghijkl","store_id":"STO_1234567890abcdefghijkl","product_id":"PROD_1234567890abcdefghijkl","return_url":"https://console.example.test/wallet"}`, Outcome: "success", Status: 200},
		{Name: "epay_quote", Group: "quote", Provider: "epay", Actor: "user", Method: "POST", Path: "/api/user/amount", Body: `{"amount":10}`, Outcome: "success", Status: 200},
		{Name: "epay_pay", Group: "pay", Provider: "epay", Actor: "user", Method: "POST", Path: "/api/user/pay", Body: `{"amount":10,"payment_method":"alipay"}`, Outcome: "success", Status: 200, Orders: 1, OrderStatus: "pending"},
		{Name: "stripe_quote", Group: "quote", Provider: "stripe", Actor: "user", Method: "POST", Path: "/api/user/stripe/amount", Body: `{"amount":10}`, Outcome: "success", Status: 200},
		{Name: "stripe_pay", Group: "pay", Provider: "stripe", Actor: "user", Method: "POST", Path: "/api/user/stripe/pay", Body: `{"amount":10,"payment_method":"stripe"}`, Outcome: "success", Status: 200, Orders: 1, OrderStatus: "pending", Calls: []string{"/v1/checkout/sessions"}},
		{Name: "creem_pay", Group: "pay", Provider: "creem", Actor: "user", Method: "POST", Path: "/api/user/creem/pay", Body: `{"product_id":"prod_test","payment_method":"creem"}`, Outcome: "success", Status: 200, Orders: 1, OrderStatus: "pending", Calls: []string{"/v1/checkouts"}},
		{Name: "waffo_quote", Group: "quote", Provider: "waffo", Actor: "user", Method: "POST", Path: "/api/user/waffo/amount", Body: `{"amount":10}`, Outcome: "success", Status: 200},
		{Name: "waffo_pay", Group: "pay", Provider: "waffo", Actor: "user", Method: "POST", Path: "/api/user/waffo/pay", Body: `{"amount":10,"pay_method_index":0}`, Outcome: "success", Status: 200, Orders: 1, OrderStatus: "pending", Calls: []string{"/api/v1/order/create"}},
		{Name: "pancake_quote", Group: "quote", Provider: "pancake", Actor: "user", Method: "POST", Path: "/api/user/waffo-pancake/amount", Body: `{"amount":10}`, Outcome: "success", Status: 200},
		{Name: "pancake_pay", Group: "pay", Provider: "pancake", Actor: "user", Method: "POST", Path: "/api/user/waffo-pancake/pay", Body: `{"amount":10}`, Outcome: "success", Status: 200, Orders: 1, OrderStatus: "pending", Calls: []string{"/v1/actions/auth/issue-session-token", "/v1/actions/checkout/create-session"}},
		{Name: "stripe_webhook", Group: "webhook", Provider: "stripe", Method: "POST", Path: "/api/stripe/webhook", Event: "paid", Outcome: "empty", Status: 200, Orders: 1, OrderStatus: "success", Credit: 1750000, Logs: 1},
		{Name: "creem_webhook", Group: "webhook", Provider: "creem", Method: "POST", Path: "/api/creem/webhook", Event: "paid", Outcome: "empty", Status: 200, Orders: 1, OrderStatus: "success", Credit: 12345, Logs: 1},
		{Name: "waffo_webhook", Group: "webhook", Provider: "waffo", Method: "POST", Path: "/api/waffo/webhook", Event: "paid", Outcome: `{"message":"success"}`, Status: 200, Orders: 1, OrderStatus: "success", Credit: 1000000, Logs: 1},
		{Name: "pancake_webhook", Group: "webhook", Provider: "pancake", Method: "POST", Path: "/api/waffo-pancake/webhook/test", Event: "paid", Outcome: "OK", Status: 200, Orders: 1, OrderStatus: "success", Credit: 1000000, Logs: 1},
		{Name: "epay_notify", Group: "webhook", Provider: "epay", Method: "POST", Path: "/api/user/epay/notify", Event: "paid", Outcome: "success", Status: 200, Orders: 1, OrderStatus: "success", Credit: 1000000, Logs: 1},
	}
}

func contractCases() []contractCase {
	cases := []contractCase{}
	for _, base := range routes() {
		if base.Group == "webhook" {
			continue
		}
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
			if base.Group == "compliance" && actor == "root_pat" {
				tc.Status = 403
				denied = true
			}
			if denied {
				tc.Outcome = "denied"
				tc.Credit = 0
				tc.Logs = 0
				tc.Calls = nil
				tc.Orders = 0
				tc.OrderStatus = ""
				if base.Group == "manual" {
					tc.Orders = 1
					tc.OrderStatus = "pending"
				}
			}
			cases = append(cases, tc)
		}
	}
	for _, base := range routes() {
		if base.Group == "quote" || base.Group == "pay" {
			for _, variant := range []string{"malformed", "amount_type", "zero", "negative", "below_min", "overflow", "wallet_full", "compliance_off", "disabled", "discount", "ratio_zero", "currency_CNY", "currency_TOKENS", "currency_CUSTOM", "exchange", "tiny_price"} {
				if base.Provider == "creem" && (variant == "amount_type" || variant == "zero" || variant == "negative" || variant == "below_min" || variant == "overflow" || variant == "tiny_price") {
					continue
				}
				tc := base
				tc.Name = base.Name + "_" + variant
				tc.Setup = variant
				reject := slices.Contains([]string{"malformed", "amount_type", "zero", "negative", "below_min", "overflow", "wallet_full", "tiny_price"}, variant)
				if variant == "disabled" && (base.Provider == "waffo" || base.Provider == "pancake" || base.Provider == "epay") && base.Group == "pay" {
					reject = true
				}
				if variant == "compliance_off" && base.Provider == "pancake" && base.Group == "pay" {
					reject = true
				}
				// Token-mode Stripe minimum scales to quota units before its fixed max check.
				if variant == "currency_TOKENS" && base.Provider == "stripe" {
					reject = true
				}
				if variant == "tiny_price" && base.Provider == "stripe" && base.Group == "pay" {
					reject = false
				}
				if reject {
					tc.Outcome = "error"
					tc.Orders = 0
					tc.OrderStatus = ""
					tc.Calls = nil
				}
				cases = append(cases, tc)
			}
			if base.Group == "pay" && base.Provider != "epay" {
				for _, variant := range []string{"upstream_error", "upstream_malformed", "upstream_empty", "insert_failure"} {
					tc := base
					tc.Name = base.Name + "_" + variant
					tc.Setup = variant
					tc.Outcome = "error"
					if variant == "insert_failure" {
						tc.Orders = 0
						tc.OrderStatus = ""
						if base.Provider != "stripe" {
							tc.Calls = nil
						}
					} else if base.Provider == "stripe" {
						tc.Orders = 0
						tc.OrderStatus = ""
					} else if base.Provider == "waffo" || base.Provider == "pancake" {
						tc.OrderStatus = "failed"
					}
					// Waffo accepts an empty redirect URL if the provider reports success.
					if variant == "upstream_empty" && (base.Provider == "waffo" || base.Provider == "stripe") {
						tc.Outcome = "success"
						tc.OrderStatus = "pending"
						tc.Orders = 1
					}
					cases = append(cases, tc)
				}
			}
		}
		if base.Group != "webhook" {
			continue
		}
		cases = append(cases, base)
		for _, variant := range []string{"repeat", "missing_signature", "bad_signature", "stale_signature", "malformed_event", "unknown_event", "unpaid", "missing_order", "foreign_provider", "already_paid", "failed_order", "wallet_full", "wrong_money", "credit_failure", "commit_failure", "compliance_off", "disabled", "read_failure"} {
			if variant == "stale_signature" && base.Provider != "stripe" && base.Provider != "pancake" {
				continue
			}
			if variant == "commit_failure" {
				continue
			} // Covered by a real transaction failure at the wallet boundary below.
			tc := base
			tc.Name = base.Provider + "_webhook_" + variant
			tc.Setup = variant
			if variant == "repeat" {
				tc.Repeat = true
				cases = append(cases, tc)
				continue
			}
			if variant == "wrong_money" || (variant == "unpaid" && base.Provider == "pancake") {
				cases = append(cases, tc)
				continue
			}
			tc.Credit = 0
			tc.Logs = 0
			tc.OrderStatus = "pending"
			switch variant {
			case "missing_signature", "bad_signature", "stale_signature":
				switch base.Provider {
				case "stripe", "waffo":
					tc.Status = 400
					tc.Outcome = "empty"
				case "creem":
					tc.Status = 401
					tc.Outcome = "empty"
				case "pancake":
					tc.Status = 401
					tc.Outcome = "invalid signature"
				case "epay":
					tc.Outcome = "fail"
				}
			case "malformed_event":
				switch base.Provider {
				case "stripe", "creem":
					tc.Status = 400
					tc.Outcome = "empty"
				case "waffo":
					tc.Outcome = `{"message":"failed"}`
				case "pancake":
					tc.Status = 401
					tc.Outcome = "invalid signature"
				case "epay":
					tc.Outcome = "fail"
				}
			case "unknown_event":
				tc.Event = "unknown"
			case "unpaid":
				tc.Event = "unpaid"
				if base.Provider == "waffo" {
					tc.OrderStatus = "failed"
				}
			case "missing_order":
				tc.Orders = 0
				tc.OrderStatus = ""
				switch base.Provider {
				case "creem":
					tc.Status = 400
				case "waffo":
					tc.Outcome = `{"message":"failed"}`
				case "epay":
					tc.Outcome = "fail"
				}
			case "foreign_provider":
				switch base.Provider {
				case "creem":
					tc.Status = 500
				case "waffo":
					tc.Outcome = `{"message":"failed"}`
				case "epay":
					tc.Outcome = "fail"
				}
			case "already_paid":
				tc.OrderStatus = "success"
			case "failed_order":
				tc.OrderStatus = "failed"
				if base.Provider == "waffo" {
					tc.Outcome = `{"message":"failed"}`
				}
				if base.Provider == "pancake" {
					tc.Status = 500
					tc.Outcome = "retry"
				}
				if base.Provider == "epay" {
					tc.Outcome = "fail"
				}
			case "wallet_full", "credit_failure":
				switch base.Provider {
				case "creem":
					tc.Status = 500
				case "waffo":
					tc.Outcome = `{"message":"failed"}`
				case "pancake":
					tc.Status = 500
					tc.Outcome = "retry"
				case "epay":
					tc.Outcome = "fail"
				}
			case "compliance_off", "disabled":
				tc.Status = 403
				tc.Outcome = "empty"
				if base.Provider == "epay" {
					tc.Status = 200
					tc.Outcome = "fail"
				}
				if base.Provider == "pancake" {
					tc.Outcome = "webhook disabled"
				}
			case "read_failure":
				tc.Status = 400
				tc.Outcome = "empty"
			}
			cases = append(cases, tc)
		}
		if slices.Contains([]string{"stripe", "creem", "pancake"}, base.Provider) {
			for _, variant := range []string{"subscription", "subscription_repeat", "subscription_foreign", "subscription_failed"} {
				tc := base
				tc.Name = base.Provider + "_" + variant
				tc.Setup = variant
				tc.Subscription = true
				tc.Credit = 0
				tc.Orders = 1
				tc.OrderStatus = "success"
				tc.Logs = 1
				if variant == "subscription_repeat" {
					tc.Repeat = true
				}
				if variant == "subscription_foreign" || variant == "subscription_failed" {
					tc.Orders = 0
					tc.OrderStatus = ""
					tc.Logs = 0
					if base.Provider == "creem" {
						tc.Status = 500
					}
					if base.Provider == "pancake" && variant == "subscription_failed" {
						tc.Status = 500
						tc.Outcome = "retry"
					}
				}
				cases = append(cases, tc)
			}
		}
	}
	for _, variant := range []string{"async_success", "async_failed", "expired", "completed_unpaid", "completed_wrong_status", "failure_then_paid", "paid_then_async", "expired_wrong_status"} {
		tc := routes()[17]
		tc.Name = "stripe_" + variant
		tc.Event = variant
		if variant != "async_success" && variant != "paid_then_async" {
			tc.Credit = 0
			tc.Logs = 0
			tc.OrderStatus = "pending"
		}
		if variant == "async_failed" || variant == "failure_then_paid" {
			tc.OrderStatus = "failed"
		}
		if variant == "expired" {
			tc.OrderStatus = "expired"
		}
		if variant == "failure_then_paid" {
			tc.Next = "paid"
			tc.Repeat = true
		}
		if variant == "paid_then_async" {
			tc.Event = "paid"
			tc.Next = "async_success"
			tc.Repeat = true
		}
		cases = append(cases, tc)
	}
	for _, variant := range []string{"wrong_env", "unknown_env", "buyer_mismatch", "external_missing", "unpaid_type"} {
		tc := routes()[20]
		tc.Name = "pancake_" + variant
		tc.Setup = variant
		tc.Credit = 0
		tc.Logs = 0
		tc.OrderStatus = "pending"
		if variant == "unknown_env" {
			tc.Path = "/api/waffo-pancake/webhook/unknown"
			tc.Status = 404
			tc.Outcome = "unknown env"
		}
		if variant == "unpaid_type" {
			tc.Event = "unknown"
		}
		cases = append(cases, tc)
	}
	for _, variant := range []string{"get", "method_switch", "get_bad_signature", "post_query_ignored", "non_success"} {
		tc := routes()[21]
		tc.Name = "epay_" + variant
		tc.Setup = variant
		if strings.HasPrefix(variant, "get") {
			tc.Method = "GET"
		}
		if variant == "get_bad_signature" {
			tc.Credit = 0
			tc.Logs = 0
			tc.OrderStatus = "pending"
			tc.Outcome = "fail"
		}
		if variant == "non_success" {
			tc.Event = "unpaid"
			tc.Credit = 0
			tc.Logs = 0
			tc.OrderStatus = "pending"
		}
		cases = append(cases, tc)
	}
	return append(append(cases, administrativeCases()...), extendedCases()...)
}

func administrativeCases() []contractCase {
	cases := []contractCase{}
	for _, group := range []string{"self", "admin_list"} {
		for _, v := range []struct {
			name, query string
			total       int
			ids         []int
		}{
			{"default", "", 4, []int{15, 14, 12, 11}}, {"page2", "?p=2&page_size=2", 4, []int{12, 11}},
			{"empty_page", "?p=99&page_size=2", 4, []int{}}, {"ignored", "?status=failed&provider=missing&user_id=999&start_timestamp=99999999999&sort=asc", 4, []int{15, 14, 12, 11}},
			{"exact", "?keyword=alpha", 1, []int{11}}, {"fuzzy", "?keyword=alpha%25", 2, []int{12, 11}},
			{"underscore", "?keyword=alpha_b", 1, []int{12}}, {"bang", "?keyword=wow!", 1, []int{14}},
			{"bad_pattern", "?keyword=%25%25", 0, nil}, {"short_pattern", "?keyword=a%25", 0, nil},
			{"many_pattern", "?keyword=ab%25cd%25ef%25", 0, nil}, {"no_matches", "?keyword=absent", 0, []int{}},
			{"ps", "?ps=2", 4, []int{15, 14}}, {"size", "?page_size=no&ps=0&size=2", 4, []int{15, 14}},
			{"precedence", "?page_size=2&ps=1&size=1", 4, []int{15, 14}}, {"negative_page", "?p=-1&page_size=2", 4, []int{15, 14}},
			{"negative_size", "?page_size=-1", 4, []int{15, 14, 12, 11}}, {"cap", "?page_size=101", 4, []int{15, 14, 12, 11}},
			{"first_value", "?keyword=alpha&keyword=absent", 1, []int{11}},
		} {
			tc := routes()[1]
			if group == "admin_list" {
				tc = routes()[2]
			}
			tc.Name = group + "_" + v.name
			tc.Path += v.query
			tc.Setup = "list_seed"
			tc.Total = v.total
			tc.IDs = v.ids
			tc.Orders = 6
			if group == "admin_list" && v.query == "" {
				tc.Total = 6
				tc.IDs = []int{16, 15, 14, 13, 12, 11}
			}
			if group == "admin_list" && v.name == "page2" {
				tc.Total = 6
				tc.IDs = []int{14, 13}
			}
			if group == "admin_list" && !strings.Contains(v.query, "keyword") && v.name != "page2" && v.name != "empty_page" && v.name != "ps" && v.name != "size" && v.name != "precedence" && v.name != "negative_page" {
				tc.Total = 6
				tc.IDs = []int{16, 15, 14, 13, 12, 11}
			}
			if group == "admin_list" && (v.name == "ps" || v.name == "size" || v.name == "precedence" || v.name == "negative_page") {
				tc.Total = 6
				tc.IDs = []int{16, 15}
			}
			if group == "admin_list" && v.name == "empty_page" {
				tc.Total = 6
			}
			if strings.HasSuffix(v.name, "pattern") {
				tc.Outcome = "api_error"
			}
			cases = append(cases, tc)
		}
	}
	for _, provider := range []string{"epay", "stripe", "creem", "waffo", "pancake"} {
		for _, v := range []string{"success", "repeat", "missing", "already_paid", "failed_order", "wallet_full", "malformed", "empty"} {
			tc := routes()[3]
			tc.Provider = provider
			tc.Name = "manual_" + provider + "_" + v
			tc.Setup = v
			if provider == "stripe" {
				tc.Credit = 1750000
			}
			if provider == "creem" {
				tc.Credit = 6172500000
			}
			if v == "repeat" {
				tc.Repeat = true
			}
			if v == "already_paid" {
				tc.Credit = 0
				tc.Logs = 1
			}
			if slices.Contains([]string{"missing", "failed_order", "wallet_full", "malformed", "empty"}, v) {
				tc.Credit = 0
				tc.Logs = 0
				tc.Outcome = "api_error"
				tc.OrderStatus = "pending"
			}
			if v == "missing" {
				tc.Orders = 0
				tc.OrderStatus = ""
			}
			if v == "failed_order" {
				tc.OrderStatus = "failed"
			}
			if v == "malformed" {
				tc.Body = "{"
			}
			if v == "empty" {
				tc.Body = `{"trade_no":""}`
			}
			cases = append(cases, tc)
		}
	}
	for _, base := range routes() {
		if !slices.Contains([]string{"catalog", "pair", "save", "compliance"}, base.Group) {
			continue
		}
		for _, v := range []string{"malformed", "missing_fields", "upstream_error", "upstream_malformed", "orphan_product", "orphan_publish", "inactive_products", "empty_catalog", "typed_creds", "partial_creds", "blank_private", "save_failure"} {
			if base.Group == "compliance" && !slices.Contains([]string{"malformed", "missing_fields"}, v) {
				continue
			}
			if base.Group == "save" && !slices.Contains([]string{"malformed", "missing_fields", "blank_private", "save_failure"}, v) {
				continue
			}
			if base.Group == "catalog" && slices.Contains([]string{"malformed", "orphan_product", "orphan_publish", "blank_private", "save_failure"}, v) {
				continue
			}
			if base.Group == "pair" && slices.Contains([]string{"inactive_products", "empty_catalog", "blank_private", "save_failure"}, v) {
				continue
			}
			tc := base
			tc.Name = base.Name + "_" + v
			tc.Setup = v
			reject := slices.Contains([]string{"malformed", "missing_fields", "upstream_error", "upstream_malformed", "orphan_product", "orphan_publish", "partial_creds", "save_failure"}, v)
			if reject {
				tc.Outcome = "error"
				if base.Group == "compliance" {
					tc.Outcome = "api_error"
				}
			}
			if v == "malformed" {
				tc.Body = "{"
				tc.Calls = nil
			}
			if v == "missing_fields" {
				tc.Body = `{}`
				tc.Calls = nil
			}
			if v == "partial_creds" {
				tc.Body = `{"merchant_id":"different"}`
				tc.Path += "?merchant_id=different"
				tc.Calls = nil
			}
			if v == "upstream_error" || v == "upstream_malformed" {
				if base.Group == "pair" {
					tc.Calls = []string{"/v1/actions/store/create-store"}
				}
			}
			if v == "orphan_product" {
				tc.Calls = []string{"/v1/actions/store/create-store", "/v1/actions/onetime-product/create-product"}
			}
			cases = append(cases, tc)
		}
	}
	return cases
}

func TestPaymentsContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	var contentCases map[string]bool
	if updating {
		if os.Getenv("PAYMENTS_CONTRACT_CONTENT_RECORD") != "" {
			contentCases = verifyContentRecording(t)
		} else {
			verifyOriginRecording(t)
		}
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
		fmt.Println("PAYMENTS_MANIFEST " + string(data))
		data, err = common.Marshal(localeCases())
		require.NoError(t, err)
		fmt.Println("PAYMENTS_LOCALES " + string(data))
		return
	}
	if !transportOverlayActive() {
		if *preflight || updating || os.Getenv("PAYMENTS_CONTRACT_LOCALE_EVIDENCE") != "" {
			t.Fatal("payments contract requires the verified SDK transport overlay")
		}
		t.Skip("payments contract requires SDK transport fixture; run the dedicated overlay command")
	}
	if name := os.Getenv("PAYMENTS_CONTRACT_CASE"); name != "" {
		if contentCases != nil {
			require.True(t, contentCases[name], "unapproved content recording case")
		}
		for _, tc := range cases {
			if tc.Name == name {
				runContractCase(t, tc, os.Getenv("PAYMENTS_CONTRACT_LANGUAGE"))
				return
			}
		}
		t.Fatalf("unknown payments case %q", name)
	}
	if path := os.Getenv("PAYMENTS_CONTRACT_LOCALE_EVIDENCE"); path != "" {
		require.False(t, updating)
		writeLocaleEvidence(t, path)
		return
	}
	for _, tc := range cases {
		if contentCases != nil && !contentCases[tc.Name] {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			output := runChild(t, tc.Name, "zh-CN", updating, *preflight)
			if *preflight {
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, "PAYMENTS_SNAPSHOT ") {
						fmt.Println(line)
					}
				}
			}
		})
	}
}

func runChild(t *testing.T, name, language string, updating, checking bool) []byte {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestPaymentsContract$", "-test.count=1", "-test.timeout=90s", "-update="+strconv.FormatBool(updating), "-payments-preflight="+strconv.FormatBool(checking))
	cmd.Env = append(os.Environ(), "PAYMENTS_CONTRACT_CASE="+name, "PAYMENTS_CONTRACT_LANGUAGE="+language, "TZ=UTC")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "case %s: %s", name, output)
	return output
}

// Recording can only modify a verified disposable archive of the pinned origin.
func verifyOriginRecording(t *testing.T) {
	t.Helper()
	require.False(t, *manifest || *preflight)
	require.Equal(t, originCommit, os.Getenv("PAYMENTS_CONTRACT_ORIGIN_RECORD"))
	root, err := filepath.EvalSymlinks(os.Getenv("PAYMENTS_CONTRACT_ORIGIN_DIR"))
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "internal/payments/contract"), cwd)
	for dir := root; ; dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		require.True(t, os.IsNotExist(err), "recording inside a checkout is forbidden")
		if filepath.Dir(dir) == dir {
			break
		}
	}
	marker, err := os.ReadFile(filepath.Join(root, ".payments-origin"))
	require.NoError(t, err)
	require.Equal(t, originCommit+"\n", string(marker))
	data, err := os.ReadFile(filepath.Join(root, ".payments-source.sha256"))
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
	env     *contracttest.Env
	log     *gorm.DB
	fake    *providerTransport
	bearers map[string]string
	key     *rsa.PrivateKey
	quota   int
	trade   string
}

func newFixture(t *testing.T, tc contractCase) *fixture {
	t.Helper()
	env := contracttest.New(t)
	constant.AnonymousRequestBodyLimitKB = 512
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.ItemsPerPage = 10
	common.QuotaPerUnit = 500000
	common.NodeName = "payments-contract-node"
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
	f := &fixture{env: env, log: logDB, key: key, trade: testTrade, quota: 1000000, bearers: map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root, "user_pat": env.UserPAT, "admin_pat": "payments-admin-pat", "root_pat": "payments-root-pat", "api_key": "sk-payments-gateway-key"}}
	for id, pat := range map[int]string{2: f.bearers["admin_pat"], 3: f.bearers["root_pat"]} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", pat).Error)
	}
	require.NoError(t, env.DB.Create(&model.Token{Id: 101, UserId: 1, Key: "payments-gateway-key", Status: 1, ExpiredTime: -1, UnlimitedQuota: true, Name: "gateway-key", Group: "default"}).Error)
	require.NoError(t, env.DB.Model(&model.User{}).Where("id IN ?", []int{1, 2, 3}).Updates(map[string]any{"email": "", "stripe_customer": "", "setting": `{}`}).Error)
	i18n.SetUserLangLoader(accountstorage.Cache().GetUserLanguage)
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

func configureCase(t *testing.T, f *fixture, tc contractCase) {
	t.Helper()
	switch tc.Setup {
	case "wallet_edge":
		f.quota = common.MaxWalletQuota - tc.Credit
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", f.quota).Error)
	case "wallet_full":
		f.quota = common.MaxWalletQuota
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", f.quota).Error)
	case "discount":
		operation_setting.GetPaymentSetting().AmountDiscount[10] = 0.8
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1.5}`))
	case "ratio_zero":
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":0}`))
	case "currency_CNY":
		operation_setting.GetGeneralSetting().QuotaDisplayType = "CNY"
	case "currency_TOKENS":
		operation_setting.GetGeneralSetting().QuotaDisplayType = "TOKENS"
	case "currency_CUSTOM":
		operation_setting.GetGeneralSetting().QuotaDisplayType = "CUSTOM"
	case "exchange":
		operation_setting.USDExchangeRate = 13
	case "cent_price":
		operation_setting.Price = 0.001
		setting.StripeUnitPrice = 0.001
		setting.WaffoUnitPrice = 0.001
		setting.WaffoPancakeUnitPrice = 0.001
	case "tiny_price":
		operation_setting.Price = 0
		setting.StripeUnitPrice = 0
		setting.WaffoUnitPrice = 0
		setting.WaffoPancakeUnitPrice = 0
	case "compliance_off":
		operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	case "disabled":
		switch tc.Provider {
		case "stripe":
			setting.StripePriceId = ""
		case "creem":
			setting.CreemWebhookSecret = ""
		case "waffo":
			setting.WaffoEnabled = false
		case "pancake":
			setting.WaffoPancakeProductID = ""
		case "epay":
			operation_setting.PayMethods = nil
		}
	case "missing_fields":
		if tc.Group == "catalog" || tc.Group == "pair" {
			setting.WaffoPancakeMerchantID = ""
			setting.WaffoPancakePrivateKey = ""
		}
	}
	switch tc.Setup {
	case "wrong_terms":
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = "old"
	case "empty_methods":
		operation_setting.PayMethods = nil
	case "duplicate_methods":
		operation_setting.PayMethods = []map[string]string{{"type": "stripe", "name": "Existing Stripe"}, {"type": "waffo_pancake", "name": "Existing Pancake"}, {"type": "waffo", "name": "Existing Waffo"}}
	case "malformed_products":
		setting.CreemProducts = "{"
	case "quota_zero":
		setting.CreemProducts = strings.ReplaceAll(setting.CreemProducts, `"quota":12345`, `"quota":0`)
	case "quota_overflow":
		setting.CreemProducts = strings.ReplaceAll(setting.CreemProducts, `"quota":12345`, `"quota":9007199254740992`)
	case "currency_EUR":
		setting.CreemProducts = strings.ReplaceAll(setting.CreemProducts, `"currency":"USD"`, `"currency":"EUR"`)
		setting.WaffoCurrency = "EUR"
	case "currency_JPY":
		setting.WaffoCurrency = "JPY"
	case "sandbox":
		setting.WaffoSandbox = true
	case "test_environment":
		setting.CreemTestMode = true
	case "test_mode_no_secret":
		setting.CreemTestMode = true
		setting.CreemWebhookSecret = ""
	case "invalid_api_key":
		setting.StripeApiSecret = "invalid"
	case "url_overrides":
		setting.WaffoNotifyUrl = "https://notify.example.test/waffo"
		setting.WaffoReturnUrl = "https://return.example.test/waffo"
	case "customer_email", "existing_email":
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("email", "existing@example.test").Error)
	case "existing_customer":
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", 1).Update("stripe_customer", "cus_existing").Error)
	}
	if strings.HasPrefix(tc.Setup, "availability_") {
		kind := strings.TrimPrefix(tc.Setup, "availability_")
		value := ""
		if strings.HasSuffix(kind, "_whitespace") {
			kind = strings.TrimSuffix(kind, "_whitespace")
			value = "  "
		}
		switch tc.Provider {
		case "stripe":
			switch kind {
			case "api":
				setting.StripeApiSecret = value
			case "webhook":
				setting.StripeWebhookSecret = value
			case "product":
				setting.StripePriceId = value
			}
		case "creem":
			switch kind {
			case "api":
				setting.CreemApiKey = value
			case "webhook":
				setting.CreemWebhookSecret = value
			case "products":
				setting.CreemProducts = value
				if value == "" {
					setting.CreemProducts = "[]"
				}
			}
		case "waffo":
			switch kind {
			case "api":
				setting.WaffoApiKey = value
			case "private":
				setting.WaffoPrivateKey = value
			case "public":
				setting.WaffoPublicCert = value
			}
		case "pancake":
			switch kind {
			case "merchant":
				setting.WaffoPancakeMerchantID = value
			case "private":
				setting.WaffoPancakePrivateKey = value
			case "product":
				setting.WaffoPancakeProductID = value
			}
		case "epay":
			switch kind {
			case "address":
				operation_setting.PayAddress = value
			case "id":
				operation_setting.EpayId = value
			case "key":
				operation_setting.EpayKey = value
			case "methods":
				operation_setting.PayMethods = nil
			}
		}
	}

	if tc.Group == "webhook" || tc.Group == "manual" {
		status := "pending"
		if tc.Setup == "already_paid" {
			status = "success"
		}
		if tc.Setup == "failed_order" {
			status = "failed"
		}
		provider := tc.Provider
		if provider == "pancake" {
			provider = "waffo_pancake"
		}
		if tc.Setup == "foreign_provider" || tc.Setup == "subscription_foreign" {
			provider = "foreign"
		}
		amount := int64(2)
		if tc.Provider == "creem" {
			amount = 12345
		}
		if tc.Setup == "invalid_stored_quota" {
			amount = 0
		}
		if tc.Subscription {
			if tc.Provider == "pancake" {
				f.trade = "WAFFO_PANCAKE_SUB-TEST"
			}
			require.NoError(t, f.env.DB.Create(&model.SubscriptionPlan{Id: 77, Title: "Contract plan", PriceAmount: 3.5, Currency: "USD", DurationUnit: "month", DurationValue: 1, Enabled: true, TotalAmount: 1000, QuotaResetPeriod: "never"}).Error)
			if tc.Setup == "subscription_failed" {
				status = "failed"
			}
			require.NoError(t, f.env.DB.Create(&model.SubscriptionOrder{Id: 11, UserId: 1, PlanId: 77, Money: 3.5, TradeNo: f.trade, PaymentMethod: tc.Provider, PaymentProvider: provider, CreateTime: 100, Status: status}).Error)
		} else if tc.Setup != "missing_order" && tc.Setup != "missing" {
			method := provider
			if tc.Provider == "epay" {
				method = "alipay"
			}
			require.NoError(t, f.env.DB.Create(&model.TopUp{Id: 11, UserId: 1, Amount: amount, Money: func() float64 {
				if tc.Setup == "invalid_stored_quota" && tc.Provider == "stripe" {
					return 0
				}
				return 3.5
			}(), TradeNo: f.trade, PaymentMethod: method, PaymentProvider: provider, CreateTime: 100, Status: status}).Error)
		}
	}
	if tc.Setup == "list_seed" || strings.HasPrefix(tc.Setup, "list_query_") {
		now := time.Now().Unix()
		rows := []model.TopUp{
			{Id: 11, UserId: 1, TradeNo: "alpha", CreateTime: now - 100, Status: "pending"},
			{Id: 12, UserId: 1, TradeNo: "alpha_b", CreateTime: now - 100, Status: "success"},
			{Id: 13, UserId: 1, TradeNo: "old", CreateTime: 1, Status: "success"},
			{Id: 14, UserId: 1, TradeNo: "wow!", CreateTime: now - 100, Status: "failed"},
			{Id: 15, UserId: 1, TradeNo: "subscription", CreateTime: now - 100, Status: "success"},
			{Id: 16, UserId: 2, TradeNo: "other", CreateTime: now - 100, Status: "pending"},
		}
		require.NoError(t, f.env.DB.Create(&rows).Error)
	}
	if strings.HasPrefix(tc.Setup, "list_query_") {
		require.NoError(t, f.env.DB.Callback().Query().Before("gorm:query").Register("payments_contract_query", func(tx *gorm.DB) {
			if tx.Statement.Table == "top_ups" {
				_, count := tx.Statement.Dest.(*int64)
				if (count && strings.HasSuffix(tc.Setup, "count")) || (!count && strings.HasSuffix(tc.Setup, "find")) {
					tx.AddError(errors.New("fixture query failure"))
				}
			}
		}))
	}
	if tc.Setup == "insert_failure" {
		require.NoError(t, f.env.DB.Callback().Create().Before("gorm:create").Register("payments_contract_insert", func(tx *gorm.DB) {
			if tx.Statement.Table == "top_ups" {
				tx.AddError(errors.New("fixture insert failure"))
			}
		}))
	}
	if tc.Setup == "credit_failure" {
		require.NoError(t, f.env.DB.Callback().Update().Before("gorm:update").Register("payments_contract_credit", func(tx *gorm.DB) {
			if tx.Statement.Table == "users" {
				if values, ok := tx.Statement.Dest.(map[string]any); ok {
					if _, ok := values["quota"]; ok {
						tx.AddError(errors.New("fixture credit failure"))
					}
				}
			}
		}))
	}
	if tc.Setup == "commit_failure" {
		require.NoError(t, f.env.DB.Callback().Update().After("gorm:update").Register("payments_contract_commit", func(tx *gorm.DB) {
			if tx.Statement.Table == "users" {
				if values, ok := tx.Statement.Dest.(map[string]any); ok {
					if _, ok := values["quota"]; ok {
						active, ok := tx.Statement.ConnPool.(gorm.TxCommitter)
						if assert.True(t, ok, "fixture must target active transaction") {
							assert.NoError(t, active.Rollback())
						}
					}
				}
			}
		}))
	}
	if tc.Setup == "save_failure" {
		require.NoError(t, f.env.DB.Callback().Create().Before("gorm:create").Register("payments_contract_options", func(tx *gorm.DB) {
			if tx.Statement.Table == "options" {
				tx.AddError(errors.New("fixture options failure"))
			}
		}))
	}
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
	case "api.creem.io", "test-api.creem.io":
		assert.Equal(p.t, setting.CreemApiKey, r.Header.Get("x-api-key"))
		assert.Equal(p.t, "/v1/checkouts", path)
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
	if p.tc.Setup == "empty_catalog" {
		body = `{"data":{"stores":[]}}`
	}
	if p.tc.Setup == "inactive_products" {
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

type httpSnapshot struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type walletRow struct {
	ID             int    `json:"id"`
	Quota          int    `json:"quota"`
	Email          string `json:"email"`
	StripeCustomer string `json:"stripe_customer"`
	Group          string `json:"group"`
}

type stepSnapshot struct {
	HTTP               httpSnapshot              `json:"http"`
	Orders             []model.TopUp             `json:"orders"`
	Wallets            []walletRow               `json:"wallets"`
	Logs               json.RawMessage           `json:"logs"`
	Events             json.RawMessage           `json:"events"`
	SubscriptionOrders []model.SubscriptionOrder `json:"subscription_orders"`
	Subscriptions      []model.UserSubscription  `json:"subscriptions"`
	Options            []model.Option            `json:"options"`
	Outbound           []outbound                `json:"outbound"`
}

func readOrders(t *testing.T, db *gorm.DB) []model.TopUp {
	t.Helper()
	rows := []model.TopUp{}
	require.NoError(t, db.Order("id").Find(&rows).Error)
	return rows
}

func requestBody(tc contractCase) string {
	if tc.Group != "pay" && tc.Group != "quote" {
		return tc.Body
	}
	if tc.Setup == "malformed" {
		return "{"
	}
	body := tc.Body
	switch tc.Setup {
	case "amount_type":
		body = strings.Replace(body, `"amount":10`, `"amount":"ten"`, 1)
	case "zero":
		body = strings.Replace(body, `"amount":10`, `"amount":0`, 1)
	case "negative":
		body = strings.Replace(body, `"amount":10`, `"amount":-1`, 1)
	case "below_min":
		body = strings.Replace(body, `"amount":10`, `"amount":0`, 1)
	case "overflow":
		body = strings.Replace(body, `"amount":10`, `"amount":9223372036854775807`, 1)
	case "currency_TOKENS":
		body = strings.Replace(body, `"amount":10`, `"amount":5000000`, 1)
	}
	return body
}

func runContractCase(t *testing.T, tc contractCase, language string) {
	f := newFixture(t, tc)
	if language == "" {
		language = "zh-CN"
	}
	if tc.Setup == "critical_rate" {
		for range 20 {
			r := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader("{"))
			r.Header.Set("Authorization", "Bearer "+f.bearers[tc.Actor])
			r.Header.Set("Content-Type", "application/json")
			r.RemoteAddr = "192.0.2.8:3456"
			response := f.env.Serve(r)
			require.Equal(t, 200, response.Code)
		}
	}
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
			body := requestBody(tc)
			if tc.Setup == "typed_creds" {
				encoded, err := common.Marshal(map[string]string{"merchant_id": "MER_1234567890abcdefghijkl", "private_key": testPrivateKey})
				require.NoError(t, err)
				body = string(encoded)
				if tc.Group == "catalog" {
					tc.Path += "?merchant_id=MER_1234567890abcdefghijkl&private_key=" + url.QueryEscape(testPrivateKey)
				}
			}
			r = httptest.NewRequest(tc.Method, tc.Path, strings.NewReader(body))
			if body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
		}
		if language != "default" {
			r.Header.Set("Accept-Language", language)
		}
		if bearer := f.bearers[tc.Actor]; bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		r.Header.Set("User-Agent", "payments-contract-agent")
		r.RemoteAddr = "192.0.2.8:3456"
		if tc.Setup == "body_limit" {
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 512*1024+1)))
			r.ContentLength = 512*1024 + 1
		}
		started := time.Now().Unix()
		response := f.env.Serve(r)
		require.Equal(t, tc.Status, response.Code, "%s", response.Body.String())
		if tc.Setup == "critical_rate" {
			require.Equal(t, "1200", response.Header().Get("Retry-After"))
		}
		wantOutcome := tc.Outcome
		if len(tc.Outcomes) > 0 {
			wantOutcome = tc.Outcomes[step]
		}
		switch wantOutcome {
		case "empty":
			require.Empty(t, response.Body.String())
		case "api", "api_error", "denied":
			var env struct {
				Success *bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &env))
			require.NotNil(t, env.Success)
			require.Equal(t, wantOutcome == "api", *env.Success, response.Body.String())
		case "success", "error":
			if tc.Group == "webhook" {
				require.Equal(t, wantOutcome, response.Body.String())
			} else {
				var env struct {
					Message string `json:"message"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &env))
				if wantOutcome == "success" {
					require.Equal(t, "success", env.Message)
				} else {
					require.NotEqual(t, "success", env.Message)
				}
			}
		default:
			require.Equal(t, wantOutcome, response.Body.String())
		}
		if tc.Provider == "waffo" && strings.HasPrefix(wantOutcome, `{"message":`) {
			require.True(t, waffoutils.Verify(response.Body.String(), response.Header().Get("X-SIGNATURE"), setting.WaffoPublicCert))
		}
		if strings.HasPrefix(tc.Setup, "list_query_") {
			require.NoError(t, f.env.DB.Callback().Query().Remove("payments_contract_query"))
		}
		orders := readOrders(t, f.env.DB)
		require.Len(t, orders, tc.Orders, "exact order count")
		credit := tc.Credit
		if len(tc.Credits) > 0 {
			credit = tc.Credits[step]
		}
		// A manual repeat keeps credit once but writes an extra zero-user receipt.
		logCount := tc.Logs
		if len(tc.LogCounts) > 0 {
			logCount = tc.LogCounts[step]
		}
		if tc.Group == "manual" && tc.Repeat && step > 0 {
			logCount++
		}
		orderStatus := tc.OrderStatus
		if len(tc.Statuses) > 0 {
			orderStatus = tc.Statuses[step]
		}
		if orderStatus != "" {
			require.Len(t, orders, 1)
			require.Equal(t, orderStatus, orders[0].Status)
		}
		wallets := []walletRow{}
		require.NoError(t, f.env.DB.Model(&model.User{}).Order("id").Find(&wallets).Error)
		require.Len(t, wallets, 3)
		for _, wallet := range wallets {
			expected := 1000000
			if wallet.ID == 1 {
				expected = f.quota + credit
			}
			require.Equal(t, expected, wallet.Quota, "wallet %d", wallet.ID)
			expectedEmail, expectedCustomer := "", ""
			if tc.Setup == "customer_email" || tc.Setup == "existing_email" {
				expectedEmail = "existing@example.test"
			}
			if tc.Setup == "existing_customer" {
				expectedCustomer = "cus_existing"
			}
			if wallet.ID == 1 && credit > 0 && tc.Group == "webhook" {
				if tc.Provider == "creem" && tc.Setup != "empty_email" && tc.Setup != "existing_email" {
					expectedEmail = "buyer@example.test"
				}
				if tc.Provider == "stripe" {
					expectedCustomer = "cus_fixture"
				}
			}
			if wallet.ID != 1 {
				expectedEmail = ""
				expectedCustomer = ""
			}
			require.Equal(t, expectedEmail, wallet.Email)
			require.Equal(t, expectedCustomer, wallet.StripeCustomer)
			require.Equal(t, "default", wallet.Group)
		}
		if tc.Group == "pay" && tc.Outcome == "success" {
			row := orders[0]
			require.Equal(t, actorID(tc.Actor), row.UserId)
			expectedAmount := int64(10)
			expectedMoney := 10.0
			if tc.Provider == "creem" {
				expectedAmount = 12345
				expectedMoney = 12.34
			}
			if tc.Setup == "cent_price" && tc.Provider != "stripe" {
				expectedMoney = 0.01
			}
			if tc.Setup == "discount" {
				expectedMoney = 12
				if tc.Provider == "stripe" {
					expectedMoney = 15
				}
				if tc.Provider == "creem" {
					expectedMoney = 12.34
				}
			}
			require.Equal(t, expectedAmount, row.Amount)
			require.Equal(t, expectedMoney, row.Money)
		}
		if tc.Group == "quote" && tc.Outcome == "success" {
			var env struct {
				Data string `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &env))
			want := "10.00"
			if tc.Setup == "discount" {
				want = "12.00"
			}
			require.Equal(t, want, env.Data)
		}
		if tc.Setup == "list_seed" && tc.Outcome == "api" {
			var env struct {
				Data struct {
					Total int `json:"total"`
					Items []struct {
						ID int `json:"id"`
					} `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &env))
			require.Equal(t, tc.Total, env.Data.Total)
			ids := []int{}
			for _, row := range env.Data.Items {
				ids = append(ids, row.ID)
			}
			require.Equal(t, tc.IDs, ids)
		}
		if tc.Provider == "epay" && tc.Group == "pay" && tc.Outcome == "success" {
			var form struct {
				Data map[string]string `json:"data"`
				URL  string            `json:"url"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &form))
			require.Equal(t, epay.GenerateParams(form.Data, operation_setting.EpayKey)["sign"], form.Data["sign"])
			require.Equal(t, "https://epay.example.test/submit.php", form.URL)
			require.Equal(t, "https://console.example.test//api/user/epay/notify", form.Data["notify_url"])
			require.Equal(t, "https://console.example.test/usage-logs", form.Data["return_url"])
			require.Equal(t, "alipay", form.Data["type"])
			require.Equal(t, "pc", form.Data["device"])
			require.Equal(t, operation_setting.EpayId, form.Data["pid"])
		}
		if tc.Group == "info" && tc.Outcome == "api" {
			var env struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &env))
			for _, provider := range []string{"epay", "stripe", "creem", "waffo", "pancake"} {
				key := map[string]string{"epay": "enable_online_topup", "stripe": "enable_stripe_topup", "creem": "enable_creem_topup", "waffo": "enable_waffo_topup", "pancake": "enable_waffo_pancake_topup"}[provider]
				expected := true
				if tc.Setup == "compliance_off" || tc.Setup == "wrong_terms" {
					expected = false
				}
				if tc.Setup == "empty_methods" && provider == "epay" {
					expected = false
				}
				if provider == tc.Provider && strings.HasPrefix(tc.Setup, "availability_") {
					expected = false
					if provider == "creem" && tc.Setup == "availability_webhook" {
						expected = true
					}
				}
				var enabled bool
				require.NoError(t, common.Unmarshal(env.Data[key], &enabled))
				require.Equal(t, expected, enabled, key)
			}
			var redemption bool
			require.NoError(t, common.Unmarshal(env.Data["enable_redemption"], &redemption))
			require.Equal(t, tc.Setup != "compliance_off" && tc.Setup != "wrong_terms", redemption)
		}
		state := httpSnapshot{Status: response.Code, Body: response.Body.String(), Headers: map[string]string{}}
		for _, name := range []string{"Cache-Control", "Pragma", "Expires", "Auth-Version", "Content-Type", "Retry-After", "Location", "Vary"} {
			if v := response.Header().Get(name); v != "" {
				state.Headers[name] = v
			}
		}
		httpID := response.Header().Get(common.RequestIdKey)
		require.NotEmpty(t, httpID)
		requestIDs[httpID] = fmt.Sprintf("<http-request-%d>", step+1)
		state.Headers[common.RequestIdKey] = "<http-request>"
		state.Body = strings.ReplaceAll(state.Body, httpID, "<http-request>")
		if signature := response.Header().Get("X-SIGNATURE"); signature != "" {
			state.Headers["X-SIGNATURE"] = "<verified-signature>"
		}
		f.fake.mu.Lock()
		calls := slices.Clone(f.fake.calls)
		f.fake.mu.Unlock()
		paths := []string{}
		for _, call := range calls {
			u, err := url.Parse(call.URL)
			require.NoError(t, err)
			paths = append(paths, u.Path)
		}
		expectedCalls := slices.Clone(tc.Calls)
		if expectedCalls == nil {
			expectedCalls = []string{}
		}
		if tc.Provider == "pancake" && tc.Group == "pay" {
			slices.Sort(paths)
			slices.Sort(expectedCalls)
		}
		require.Equal(t, expectedCalls, paths, "exact outbound call count and order")
		for i := range orders {
			if tc.Group == "pay" {
				require.NotEmpty(t, orders[i].TradeNo)
				require.NotEqual(t, testTrade, orders[i].TradeNo)
				generated := orders[i].TradeNo
				state.Body = strings.ReplaceAll(state.Body, generated, "<generated-order>")
				for j := range calls {
					calls[j].Body = strings.ReplaceAll(calls[j].Body, generated, "<generated-order>")
				}
				orders[i].TradeNo = "<generated-order>"
			}
			if orders[i].CreateTime > 100 {
				if tc.Setup != "list_seed" && !strings.HasPrefix(tc.Setup, "list_query_") {
					require.GreaterOrEqual(t, orders[i].CreateTime, started)
				}
				state.Body = strings.ReplaceAll(state.Body, `"create_time":`+strconv.FormatInt(orders[i].CreateTime, 10), `"create_time":1`)
				orders[i].CreateTime = 1
			}
			if orders[i].CompleteTime > 0 {
				require.GreaterOrEqual(t, orders[i].CompleteTime, started-5)
				state.Body = strings.ReplaceAll(state.Body, `"complete_time":`+strconv.FormatInt(orders[i].CompleteTime, 10), `"complete_time":1`)
				orders[i].CompleteTime = 1
			}
		}
		// Remote-first Stripe can emit a reference even if the local insert fails.
		for i := range calls {
			calls[i].Body = normalizeOutgoing(t, calls[i], tc)
		}
		if tc.Provider == "pancake" && tc.Group == "pay" {
			slices.SortFunc(calls, func(a, b outbound) int { return strings.Compare(a.URL, b.URL) })
		}
		logs := []model.Log{}
		require.NoError(t, f.log.Order("id").Find(&logs).Error)
		require.Len(t, logs, logCount)
		for i := range logs {
			require.Equal(t, 1, logs[i].Type)
			require.Zero(t, logs[i].Quota)
			require.NotEmpty(t, logs[i].Content)
			require.NotEmpty(t, logs[i].RequestId)
			require.NotContains(t, requestIDs, logs[i].RequestId)
			wantUser := 1
			if tc.Group == "manual" && ((tc.Repeat && i > 0) || tc.Setup == "already_paid") {
				wantUser = 0
			}
			require.Equal(t, wantUser, logs[i].UserId)
			logs[i].CreatedAt = 1
			logs[i].RequestId = fmt.Sprintf("<usage-request-%d>", i+1)
			if logs[i].Other != "" {
				var other map[string]json.RawMessage
				require.NoError(t, common.UnmarshalJsonStr(logs[i].Other, &other))
				if raw := other["admin_info"]; raw != nil {
					var admin map[string]json.RawMessage
					require.NoError(t, common.Unmarshal(raw, &admin))
					var actual string
					require.NoError(t, common.Unmarshal(admin["server_ip"], &actual))
					require.Equal(t, common.GetIp(), actual)
					admin["server_ip"] = json.RawMessage(`"<server-ip>"`)
					encoded, err := common.Marshal(admin)
					require.NoError(t, err)
					other["admin_info"] = encoded
					encoded, err = common.Marshal(other)
					require.NoError(t, err)
					logs[i].Other = string(encoded)
				}
			}
		}
		events := []model.AuditLog{}
		require.NoError(t, f.log.Order("id").Find(&events).Error)
		pat := strings.HasSuffix(tc.Actor, "_pat")
		operation := (strings.HasPrefix(tc.Actor, "admin") || strings.HasPrefix(tc.Actor, "root")) && tc.Method != "GET" && slices.Contains([]string{"manual", "compliance", "pair", "save"}, tc.Group) && tc.Status != 401
		// A lower-role denial occurs before admin fallback starts.
		if tc.Status == 403 && !(tc.Group == "compliance" && tc.Actor == "root_pat") {
			operation = false
		}
		wantEvents := 0
		if pat {
			wantEvents++
		}
		if operation {
			wantEvents++
		}
		require.Len(t, events, wantEvents*(step+1))
		for i := range events {
			row := &events[i]
			require.Contains(t, requestIDs, row.RequestId)
			row.RequestId = requestIDs[row.RequestId]
			require.NotEmpty(t, row.EventId)
			row.EventId = fmt.Sprintf("<event-%d>", i+1)
			row.CreatedAt = 1
		}
		subscriptions := []model.UserSubscription{}
		subscriptionOrders := []model.SubscriptionOrder{}
		require.NoError(t, f.env.DB.Order("id").Find(&subscriptions).Error)
		require.NoError(t, f.env.DB.Order("id").Find(&subscriptionOrders).Error)
		wantSubscriptions := 0
		if tc.Subscription && tc.Logs > 0 {
			wantSubscriptions = 1
		}
		require.Len(t, subscriptions, wantSubscriptions)
		for i := range subscriptionOrders {
			if subscriptionOrders[i].CompleteTime > 0 {
				subscriptionOrders[i].CompleteTime = 1
			}
		}
		for i := range subscriptions {
			row := &subscriptions[i]
			require.EqualValues(t, 1000, row.AmountTotal)
			require.Zero(t, row.AmountUsed)
			require.Equal(t, "active", row.Status)
			require.Equal(t, 1, row.UserId)
			require.Greater(t, row.EndTime, row.StartTime)
			row.EndTime -= row.StartTime
			row.StartTime = 1
			row.CreatedAt = 1
			row.UpdatedAt = 1
			if row.LastResetTime > 0 {
				row.LastResetTime = 1
			}
			if row.NextResetTime > 0 {
				row.NextResetTime = 1
			}
		}
		options := []model.Option{}
		require.NoError(t, f.env.DB.Order("key").Find(&options).Error)
		for i := range options {
			if options[i].Key == "payment_setting.compliance_confirmed_at" {
				require.NotEmpty(t, options[i].Value)
				state.Body = strings.ReplaceAll(state.Body, `"confirmed_at":`+options[i].Value, `"confirmed_at":1`)
				options[i].Value = "1"
			}
			if strings.Contains(strings.ToLower(options[i].Key), "privatekey") {
				require.Equal(t, testPrivateKey, options[i].Value)
				options[i].Value = "<fixture-private-key>"
			}
		}
		if tc.Group == "save" && tc.Outcome == "success" {
			require.Len(t, options, 4)
			require.Equal(t, productID, setting.WaffoPancakeProductID)
			require.Equal(t, testPrivateKey, setting.WaffoPancakePrivateKey)
		}
		if tc.Group == "compliance" && tc.Outcome == "api" {
			require.Len(t, options, 5)
			require.True(t, operation_setting.IsPaymentComplianceConfirmed())
		}
		if tc.Group == "save" && tc.Setup == "save_failure" {
			require.Empty(t, options)
		}
		if tc.Group == "pair" || tc.Group == "catalog" {
			require.Empty(t, options)
		}
		state.Body = redactFixtureResponse(t, state.Body, tc)
		logJSON, err := common.Marshal(logs)
		require.NoError(t, err)
		eventJSON, err := common.Marshal(events)
		require.NoError(t, err)
		snapshot := stepSnapshot{state, orders, wallets, logJSON, eventJSON, subscriptionOrders, subscriptions, options, calls}
		encoded, err := common.Marshal(snapshot)
		require.NoError(t, err)
		for _, secret := range []string{setting.StripeApiSecret, setting.StripeWebhookSecret, setting.CreemApiKey, setting.CreemWebhookSecret, operation_setting.EpayKey, testPrivateKey, "test-only-checkout-token"} {
			if secret != "" {
				require.NotContains(t, string(encoded), secret, "secret in contract snapshot")
			}
		}
		steps = append(steps, snapshot)
		var n int64
		require.NoError(t, f.env.DB.Model(&model.Log{}).Count(&n).Error)
		require.EqualValues(t, 1, n)
		require.NoError(t, f.env.DB.Model(&model.AuditLog{}).Count(&n).Error)
		require.EqualValues(t, 1, n)
	}
	encoded, err := common.Marshal(steps)
	require.NoError(t, err)
	if *preflight {
		if directory := os.Getenv("PAYMENTS_CONTRACT_STATE_EVIDENCE"); directory != "" {
			require.NoError(t, os.MkdirAll(directory, 0o700))
			file, err := os.OpenFile(filepath.Join(directory, tc.Name+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			require.NoError(t, err)
			_, err = file.Write(append(encoded, '\n'))
			require.NoError(t, err)
			require.NoError(t, file.Close())
		}
		httpData, err := common.Marshal(steps[0].HTTP)
		require.NoError(t, err)
		fmt.Printf("PAYMENTS_HTTP %s %s\n", tc.Name, base64.StdEncoding.EncodeToString(httpData))
		fmt.Printf("PAYMENTS_SNAPSHOT %s %x\n", tc.Name, sha256.Sum256(encoded))
		return
	}
	projectInheritedSearch(t, tc, steps)
	contracttest.GoldenValue(t, "payments_"+tc.Name, steps)
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
				if strings.HasPrefix(line, "PAYMENTS_HTTP ") {
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

func extendedCases() []contractCase {
	cases := []contractCase{}
	for _, base := range routes() {
		if base.Group == "webhook" {
			tc := base
			tc.Name = base.Provider + "_webhook_late_paid"
			tc.Repeat = true
			tc.Event = "unpaid"
			tc.Next = "paid"
			tc.Credits = []int{0, base.Credit}
			tc.LogCounts = []int{0, 1}
			tc.Statuses = []string{"pending", "success"}
			tc.Outcomes = []string{base.Outcome, base.Outcome}
			if base.Provider == "pancake" {
				tc.Event = "unknown"
			}
			if base.Provider == "waffo" {
				tc.Credits = []int{0, 0}
				tc.LogCounts = []int{0, 0}
				tc.Statuses = []string{"failed", "failed"}
				tc.Outcomes = []string{`{"message":"success"}`, `{"message":"failed"}`}
			}
			cases = append(cases, tc)
		}
	}

	for _, base := range routes() {
		if base.Group == "webhook" {
			tc := base
			tc.Name = base.Provider + "_webhook_body_limit"
			tc.Setup = "body_limit"
			tc.Status = 413
			tc.Outcome = "empty"
			tc.Credit = 0
			tc.Logs = 0
			tc.OrderStatus = "pending"
			cases = append(cases, tc)
		}
		if base.Group == "pay" {
			tc := base
			tc.Name = base.Provider + "_pay_critical_rate"
			tc.Setup = "critical_rate"
			tc.Status = 429
			tc.Outcome = "empty"
			tc.Orders = 0
			tc.OrderStatus = ""
			tc.Calls = nil
			cases = append(cases, tc)
		}
	}

	for _, base := range routes() {
		if base.Group == "self" || base.Group == "admin_list" {
			for _, v := range []string{"count", "find"} {
				tc := base
				tc.Name = base.Name + "_query_failure_" + v
				tc.Setup = "list_query_" + v
				tc.Orders = 6
				tc.Outcome = "api_error"
				cases = append(cases, tc)
			}
		}
	}

	for _, base := range routes() {
		if base.Group == "info" {
			for _, provider := range []string{"epay", "stripe", "creem", "waffo", "pancake"} {
				for _, missing := range map[string][]string{
					"stripe":  {"api", "webhook", "product", "api_whitespace", "webhook_whitespace"},
					"creem":   {"api", "webhook", "products", "api_whitespace", "products_whitespace"},
					"waffo":   {"api", "private", "public", "api_whitespace", "public_whitespace"},
					"pancake": {"merchant", "private", "product", "merchant_whitespace", "private_whitespace"},
					"epay":    {"address", "id", "key", "methods", "key_whitespace"},
				}[provider] {
					tc := base
					tc.Name = "availability_" + provider + "_" + missing
					tc.Provider = provider
					tc.Setup = "availability_" + missing
					cases = append(cases, tc)
				}
			}
			for _, v := range []string{"compliance_off", "wrong_terms", "duplicate_methods", "empty_methods", "malformed_products"} {
				tc := base
				tc.Name = "info_" + v
				tc.Setup = v
				cases = append(cases, tc)
			}
		}
		if base.Group == "quote" || base.Group == "pay" {
			if base.Provider == "stripe" {
				tc := base
				tc.Name = base.Name + "_maximum"
				tc.Body = strings.ReplaceAll(tc.Body, `"amount":10`, `"amount":10001`)
				tc.Outcome = "error"
				tc.Orders = 0
				tc.OrderStatus = ""
				tc.Calls = nil
				cases = append(cases, tc)
			}
			if base.Provider != "creem" {
				tc := base
				tc.Name = base.Name + "_cent_boundary"
				tc.Setup = "cent_price"
				if base.Group == "quote" {
					tc.Outcome = "error"
					tc.Orders = 0
					tc.OrderStatus = ""
					tc.Calls = nil
				}
				cases = append(cases, tc)
			}
		}
		if base.Group == "pay" {
			switch base.Provider {
			case "epay", "stripe", "creem":
				tc := base
				tc.Name = base.Name + "_wrong_method"
				tc.Body = strings.ReplaceAll(strings.ReplaceAll(tc.Body, `"alipay"`, `"foreign"`), `"`+base.Provider+`"`, `"foreign"`)
				tc.Outcome = "error"
				tc.Orders = 0
				tc.OrderStatus = ""
				tc.Calls = nil
				cases = append(cases, tc)
			}
			if base.Provider == "stripe" {
				for _, v := range []string{"bad_success_url", "bad_cancel_url", "customer_email", "existing_customer", "invalid_api_key"} {
					tc := base
					tc.Name = base.Name + "_" + v
					tc.Setup = v
					if strings.HasPrefix(v, "bad_") {
						field := "success_url"
						if v == "bad_cancel_url" {
							field = "cancel_url"
						}
						tc.Body = strings.TrimSuffix(tc.Body, "}") + `,"` + field + `":"https://untrusted.example.test/"}`
						tc.Status = 400
						tc.Outcome = "error"
						tc.Orders = 0
						tc.OrderStatus = ""
						tc.Calls = nil
					}
					if v == "invalid_api_key" {
						tc.Outcome = "error"
						tc.Orders = 0
						tc.OrderStatus = ""
						tc.Calls = nil
					}
					cases = append(cases, tc)
				}
			}
			if base.Provider == "creem" {
				for _, v := range []string{"empty_product", "unknown_product", "malformed_products", "quota_zero", "quota_overflow", "currency_EUR", "test_environment", "existing_email"} {
					tc := base
					tc.Name = base.Name + "_" + v
					tc.Setup = v
					if v == "empty_product" {
						tc.Body = `{"product_id":"","payment_method":"creem"}`
					}
					if v == "unknown_product" {
						tc.Body = `{"product_id":"missing","payment_method":"creem"}`
					}
					if slices.Contains([]string{"empty_product", "unknown_product", "malformed_products", "quota_zero", "quota_overflow"}, v) {
						tc.Outcome = "error"
						tc.Orders = 0
						tc.OrderStatus = ""
						tc.Calls = nil
					}
					cases = append(cases, tc)
				}
			}
			if base.Provider == "waffo" {
				for _, v := range []string{"index_negative", "index_large", "legacy_invalid", "legacy_valid", "auto_method", "sandbox", "currency_JPY", "currency_EUR", "url_overrides", "bad_response_signature"} {
					tc := base
					tc.Name = base.Name + "_" + v
					tc.Setup = v
					switch v {
					case "index_negative":
						tc.Body = `{"amount":10,"pay_method_index":-1}`
					case "index_large":
						tc.Body = `{"amount":10,"pay_method_index":99}`
					case "legacy_invalid":
						tc.Body = `{"amount":10,"pay_method_type":"unknown","pay_method_name":"unknown"}`
					case "legacy_valid":
						tc.Body = `{"amount":10,"pay_method_type":"CARD","pay_method_name":"VISA"}`
					case "auto_method":
						tc.Body = `{"amount":10}`
					}
					if slices.Contains([]string{"index_negative", "index_large", "legacy_invalid"}, v) {
						tc.Outcome = "error"
						tc.Orders = 0
						tc.OrderStatus = ""
						tc.Calls = nil
					}
					if v == "bad_response_signature" {
						tc.Outcome = "error"
						tc.OrderStatus = "failed"
					}
					cases = append(cases, tc)
				}
			}
		}
		if base.Group == "webhook" {
			for _, v := range []string{"wallet_edge", "invalid_stored_quota", "commit_failure"} {
				tc := base
				tc.Name = base.Provider + "_webhook_" + v
				tc.Setup = v
				if v != "wallet_edge" {
					tc.Credit = 0
					tc.Logs = 0
					tc.OrderStatus = "pending"
					switch base.Provider {
					case "creem":
						tc.Status = 500
					case "waffo":
						tc.Outcome = `{"message":"failed"}`
					case "pancake":
						tc.Status = 500
						tc.Outcome = "retry"
					case "epay":
						tc.Outcome = "fail"
					}
				}
				cases = append(cases, tc)
			}
			if base.Provider == "creem" {
				for _, v := range []string{"existing_email", "empty_email", "not_onetime", "missing_request", "data_type", "test_mode_no_secret"} {
					tc := base
					tc.Name = "creem_webhook_" + v
					tc.Setup = v
					if slices.Contains([]string{"not_onetime", "missing_request", "data_type", "test_mode_no_secret"}, v) {
						tc.Credit = 0
						tc.Logs = 0
						tc.OrderStatus = "pending"
					}
					if v == "missing_request" || v == "data_type" {
						tc.Status = 400
					}
					if v == "test_mode_no_secret" {
						tc.Status = 403
					}
					cases = append(cases, tc)
				}
			}
			if base.Provider == "pancake" {
				for _, v := range []string{"prod", "mode_case", "data_type"} {
					tc := base
					tc.Name = "pancake_webhook_" + v
					tc.Setup = v
					if v == "prod" {
						tc.Path = "/api/waffo-pancake/webhook/prod"
					}
					if v == "data_type" {
						tc.Status = 401
						tc.Outcome = "invalid signature"
						tc.Credit = 0
						tc.Logs = 0
						tc.OrderStatus = "pending"
					}
					cases = append(cases, tc)
				}
			}
		}
	}
	return cases
}

// Inspecting the SDK client binding prevents any provider call if the overlay was omitted.
// This is read-only reflection on the pinned SDK, without unsafe or monkeypatching.
func transportOverlayActive() bool {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	http.DefaultTransport = &providerTransport{}
	transport := waffonet.NewDefaultHttpTransportWithTimeouts(10000, 30000)
	value := reflect.ValueOf(transport).Elem().FieldByName("client").Elem().FieldByName("Transport")
	return !value.IsNil() && value.Elem().Type() == reflect.TypeFor[*providerTransport]()
}

// Only the three explicitly accepted inherited search messages are projected to their origin bytes.
// The observed response is asserted before projection; preflight and locale evidence retain actual bytes.
func projectInheritedSearch(t *testing.T, tc contractCase, steps []stepSnapshot) {
	t.Helper()
	original := false
	data, err := os.ReadFile(filepath.Join("..", "..", "..", ".payments-origin"))
	if err == nil {
		require.Equal(t, originCommit+"\n", string(data))
		original = true
	} else {
		require.True(t, os.IsNotExist(err))
	}
	for _, pair := range []struct{ Suffix, Origin, Current string }{
		{"bad_pattern", "\u641c\u7d22\u6a21\u5f0f\u4e2d\u4e0d\u5141\u8bb8\u5305\u542b\u8fde\u7eed\u7684 % \u901a\u914d\u7b26", "search pattern must not contain consecutive % wildcards"},
		{"many_pattern", "\u641c\u7d22\u6a21\u5f0f\u4e2d\u6700\u591a\u5141\u8bb8\u5305\u542b 2 \u4e2a % \u901a\u914d\u7b26", "search pattern allows at most 2 % wildcards"},
		{"short_pattern", "\u4f7f\u7528\u6a21\u7cca\u641c\u7d22\u65f6\uff0c\u5173\u952e\u8bcd\u957f\u5ea6\u81f3\u5c11\u4e3a 2 \u4e2a\u5b57\u7b26", "fuzzy search keyword must be at least 2 characters"},
	} {
		if tc.Name != "self_"+pair.Suffix && tc.Name != "admin_list_"+pair.Suffix {
			continue
		}
		for i := range steps {
			var response struct {
				Message string `json:"message"`
			}
			require.NoError(t, common.UnmarshalJsonStr(steps[i].HTTP.Body, &response))
			if original {
				require.Equal(t, pair.Origin, response.Message)
				continue
			}
			require.Equal(t, pair.Current, response.Message, "inherited exception must not hide a new response delta")
			oldJSON, err := common.Marshal(pair.Current)
			require.NoError(t, err)
			newJSON, err := common.Marshal(pair.Origin)
			require.NoError(t, err)
			needle := `"message":` + string(oldJSON)
			require.Equal(t, 1, strings.Count(steps[i].HTTP.Body, needle))
			steps[i].HTTP.Body = strings.Replace(steps[i].HTTP.Body, needle, `"message":`+string(newJSON), 1)
		}
	}
}

func localeCases() []string {
	return []string{
		"epay_quote_zero", "epay_quote_overflow", "epay_pay_wrong_method", "epay_pay_disabled", "stripe_pay_below_min", "stripe_pay_maximum",
		"stripe_pay_bad_success_url", "stripe_pay_bad_cancel_url", "stripe_pay_wrong_method", "stripe_pay_wallet_full",
		"creem_pay_malformed", "creem_pay_empty_product", "creem_pay_unknown_product", "creem_pay_malformed_products", "creem_pay_quota_zero", "creem_pay_quota_overflow",
		"creem_pay_upstream_error", "waffo_pay_disabled", "waffo_pay_index_negative", "waffo_pay_upstream_error", "pancake_pay_compliance_off",
		"manual_epay_empty", "manual_epay_missing", "manual_epay_failed_order", "manual_epay_wallet_full", "compliance_missing_fields", "access_compliance_root_pat",
		"catalog_missing_fields", "pair_orphan_product", "save_missing_fields", "save_save_failure", "self_bad_pattern", "self_many_pattern", "self_short_pattern",
	}
}

// verifyContentRecording permits only the host-reviewed strings-only recording package.
func verifyContentRecording(t *testing.T) map[string]bool {
	t.Helper()
	require.Equal(t, "approved", os.Getenv("PAYMENTS_CONTRACT_CONTENT_RECORD"))
	require.False(t, *manifest || *preflight)
	require.Empty(t, os.Getenv("PAYMENTS_CONTRACT_LOCALE_EVIDENCE"))
	data, err := os.ReadFile(os.Getenv("PAYMENTS_CONTRACT_CONTENT_INPUTS"))
	require.NoError(t, err)
	var input struct {
		Root    string
		Sources map[string]string
		Cases   []string
	}
	require.NoError(t, common.Unmarshal(data, &input))
	require.NotEmpty(t, input.Sources)
	require.NotEmpty(t, input.Cases)
	root, err := filepath.EvalSymlinks(input.Root)
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "internal/payments/contract"), cwd)
	for name, digest := range input.Sources {
		require.False(t, filepath.IsAbs(name) || strings.Contains(name, ".."))
		source, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(source)), name)
	}
	allowed := map[string]bool{}
	for _, name := range input.Cases {
		require.False(t, allowed[name])
		allowed[name] = true
	}
	return allowed
}

package settings

import (
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/settings"
	systemruntime "github.com/QuantumNous/new-api/internal/adapter/runtime/system"
	runtime "github.com/QuantumNous/new-api/internal/adapter/settings/settings"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	"github.com/QuantumNous/new-api/internal/settings/get_about"
	"github.com/QuantumNous/new-api/internal/settings/get_home_page_content"
	"github.com/QuantumNous/new-api/internal/settings/get_notice"
	"github.com/QuantumNous/new-api/internal/settings/get_options"
	"github.com/QuantumNous/new-api/internal/settings/get_privacy_policy"
	"github.com/QuantumNous/new-api/internal/settings/get_status"
	"github.com/QuantumNous/new-api/internal/settings/get_user_agreement"
	"github.com/QuantumNous/new-api/internal/settings/update_option"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	GetOptions         get_options.Handler
	UpdateOption       update_option.Handler
	GetStatus          get_status.Handler
	GetNotice          get_notice.Handler
	GetAbout           get_about.Handler
	GetHomePageContent get_home_page_content.Handler
	GetUserAgreement   get_user_agreement.Handler
	GetPrivacyPolicy   get_privacy_policy.Handler
}

func NewHTTP() HTTP {
	core := Default()
	audit := auditapp.NewRecording()
	public := runtime.PublicConfiguration{BillingFX: gateway.BillingFX, Runtime: systemruntime.Facts{}}
	return HTTP{
		GetOptions: get_options.NewHandler(get_options.NewUsecase(runtime.Editor{})),
		UpdateOption: update_option.NewHandler(update_option.NewUsecase(runtime.Validation{}, core.Write), func(c *gin.Context, key string) {
			audit.Manage(c, c.GetInt("id"), "option.update", map[string]any{"key": key})
		}),
		GetStatus:          get_status.NewHandler(get_status.NewUsecase(public), public.Lock),
		GetNotice:          get_notice.NewHandler(func() string { return public.Content("Notice") }),
		GetAbout:           get_about.NewHandler(func() string { return public.Content("About") }),
		GetHomePageContent: get_home_page_content.NewHandler(func() string { return public.Content("HomePageContent") }),
		GetUserAgreement:   get_user_agreement.NewHandler(public.UserAgreement),
		GetPrivacyPolicy:   get_privacy_policy.NewHandler(public.PrivacyPolicy),
	}
}

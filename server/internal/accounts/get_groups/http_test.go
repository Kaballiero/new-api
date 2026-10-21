package get_groups

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAccountGroups struct{}

func (fakeAccountGroups) UserGroup(int, bool) (string, error) { return "vip", nil }

// The generated "user group" description and "auto" ratio are written in the
// request language; configured descriptions are kept.
func TestForUserLocalizesGeneratedLabels(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	groups := domain.Groups{
		Labels:        domain.GroupLabels{UserGroup: "unused", AutoRatio: "unused"},
		UsableCopy:    func() map[string]string { return map[string]string{"default": "Default", "auto": "Auto routing"} },
		SpecialUsable: func(string) (map[string]string, bool) { return nil, false },
		RatiosCopy:    func() map[string]float64 { return map[string]float64{"default": 1, "vip": 2} },
		Ratio:         func(_, group string) float64 { return map[string]float64{"default": 1, "vip": 2}[group] },
		Description:   func(string) string { return "Auto routing" },
	}
	router := gin.New()
	router.GET("/groups", NewHTTP(NewUsecase(groups, fakeAccountGroups{})).ForUser)
	for lang, labels := range map[string][2]string{"zh-CN": {"用户分组", "自动"}, "ru": {"Группа пользователя", "Авто"}} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/groups", nil)
		request.Header.Set("Accept-Language", lang)
		router.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"success":true,"message":"","data":{
			"default":{"ratio":1,"desc":"Default"},
			"vip":{"ratio":2,"desc":"`+labels[0]+`"},
			"auto":{"ratio":"`+labels[1]+`","desc":"Auto routing"}}}`, recorder.Body.String(), lang)
	}
}

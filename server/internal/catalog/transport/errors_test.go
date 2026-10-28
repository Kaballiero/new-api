package transport

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/catalog/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expectations repeat the legacy failure-kind mapping of the catalog
// handlers (internal/adapter/http/catalog): status, code and message.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	staleVendor := fmt.Errorf("%w: target vendor does not exist", domain.ErrVendorConflict)
	cases := []struct {
		name string
		err  error
		want string
		code int
	}{
		{"model missing", domain.ErrModelNotFound, `{"success":false,"code":"model_not_found","message":"model not found"}`, http.StatusNotFound},
		{"vendor missing", domain.RejectVendorChange(errors.New("record not found"), true), `{"success":false,"code":"vendor_not_found","message":"vendor not found"}`, http.StatusNotFound},
		{"model id", domain.ErrModelIDMissing, `{"success":false,"code":"model_id_missing","message":"Model ID is missing"}`, http.StatusBadRequest},
		{"model name empty", domain.ErrModelNameEmpty, `{"success":false,"code":"model_name_empty","message":"Model name cannot be empty"}`, http.StatusBadRequest},
		{"model name required", domain.ErrModelNameRequired, `{"success":false,"message":"Model name cannot be empty"}`, http.StatusOK},
		{"model name exists", domain.ErrModelNameExists, `{"success":false,"code":"model_name_exists","message":"Model name already exists"}`, http.StatusConflict},
		{"vendor id", domain.ErrVendorIDMissing, `{"success":false,"code":"vendor_id_missing","message":"Vendor ID is missing"}`, http.StatusBadRequest},
		{"vendor name empty", domain.ErrVendorNameEmpty, `{"success":false,"code":"vendor_name_empty","message":"Vendor name cannot be empty"}`, http.StatusBadRequest},
		{"vendor name exists", domain.ErrVendorNameExists, `{"success":false,"code":"vendor_name_exists","message":"Vendor name already exists"}`, http.StatusConflict},
		{"square state", domain.ErrInvalidSquareState, `{"success":false,"code":"invalid_params","message":"Invalid model square state"}`, http.StatusBadRequest},
		{"pagination", domain.ErrInvalidPagination, `{"success":false,"code":"invalid_params","message":"Invalid pagination"}`, http.StatusBadRequest},
		{"pricing root only", domain.ErrPricingRootOnly, `{"success":false,"message":"Model pricing is managed by a super administrator."}`, http.StatusForbidden},
		{"sync source changed", domain.ErrSyncSourceChanged, `{"success":false,"message":"Upstream metadata changed; preview again"}`, http.StatusConflict},
		{"sync model unavailable", domain.ErrSyncModelUnavailable, `{"success":false,"message":"Selected upstream model is no longer available"}`, http.StatusConflict},
		{"sync conflict", domain.RejectSync(fmt.Errorf("%w: gpt", domain.ErrMetadataSyncConflict)), `{"success":false,"message":"metadata changed; preview again before applying: gpt"}`, http.StatusConflict},
		{"sync rejected", domain.RejectSync(errors.New("select fields to update")), `{"success":false,"message":"select fields to update"}`, http.StatusBadRequest},
		{"vendor rejected", domain.RejectVendorChange(errors.New("vendor name is required"), false), `{"success":false,"message":"vendor name is required"}`, http.StatusBadRequest},
		{"vendor conflict", domain.RejectVendorChange(staleVendor, false), `{"success":false,"code":"VENDOR_CONFLICT","message":"vendor data changed; preview again before applying: target vendor does not exist"}`, http.StatusConflict},
		{"vendor referenced", domain.RejectVendorChange(&domain.VendorReferenceError{Counts: map[int]int64{3: 2}}, false), `{"success":false,"code":"VENDOR_REFERENCED","message":"vendors are still referenced by models; transfer or clear their assignments first","reference_counts":{"3":2}}`, http.StatusConflict},
		{"internal", domain.Internal(errors.New("disk full")), `{"success":false,"code":"internal_error","message":"disk full"}`, http.StatusInternalServerError},
		{"plain", domain.ErrInvalidVisibility, `{"success":false,"message":"invalid catalog visibility"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			WriteError(c, tc.err)
			assert.Equal(t, tc.code, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

// Messages that were hardcoded in Chinese keep their Chinese text for zh-CN.
func TestWriteErrorLocalizesMessage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	for lang, want := range map[string]string{"ru": "Название поставщика уже существует", "zh-CN": "供应商名称已存在"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Set(string(constant.ContextKeyLanguage), lang)
		WriteError(c, domain.ErrVendorNameExists)
		assert.Equal(t, http.StatusConflict, recorder.Code)
		assert.JSONEq(t, `{"success":false,"code":"vendor_name_exists","message":"`+want+`"}`, recorder.Body.String(), lang)
	}
}

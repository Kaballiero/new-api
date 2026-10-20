package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// openTokenRestTestDB sets up an in-memory SQLite DB with both Token and User
// tables migrated (User is referenced by token-by-id queries).
func openTokenRestTestDB(t *testing.T) {
	t.Helper()
	db := setupTokenControllerTestDB(t) // reuses token_test.go helper (Token migrated)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate user: %v", err)
	}
}

func TestGetToken_NotFound_404(t *testing.T) {
	openTokenRestTestDB(t)
	ctx, rec := newRestContext(t, http.MethodGet, "/api/token/9999", nil,
		gin.Params{{Key: "id", Value: "9999"}}, common.RoleCommonUser)
	GetToken(ctx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status got %d want 404 body=%s", rec.Code, rec.Body.String())
	}
	if c := decodeRestError(t, rec).Code; c != "token_not_found" {
		t.Errorf("code: got %q want token_not_found", c)
	}
}

func TestGetToken_BadId_400(t *testing.T) {
	openTokenRestTestDB(t)
	ctx, rec := newRestContext(t, http.MethodGet, "/api/token/abc", nil,
		gin.Params{{Key: "id", Value: "abc"}}, common.RoleCommonUser)
	GetToken(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status got %d want 400", rec.Code)
	}
	if c := decodeRestError(t, rec).Code; c != "invalid_params" {
		t.Errorf("code: got %q want invalid_params", c)
	}
}

func TestGetTokenKey_NotFound_404(t *testing.T) {
	openTokenRestTestDB(t)
	ctx, rec := newRestContext(t, http.MethodPost, "/api/token/9999/key", nil,
		gin.Params{{Key: "id", Value: "9999"}}, common.RoleCommonUser)
	GetTokenKey(ctx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status got %d want 404", rec.Code)
	}
	if c := decodeRestError(t, rec).Code; c != "token_not_found" {
		t.Errorf("code: got %q want token_not_found", c)
	}
}

func TestDeleteToken_BadId_400(t *testing.T) {
	openTokenRestTestDB(t)
	ctx, rec := newRestContext(t, http.MethodDelete, "/api/token/xx", nil,
		gin.Params{{Key: "id", Value: "xx"}}, common.RoleCommonUser)
	DeleteToken(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status got %d want 400", rec.Code)
	}
	if c := decodeRestError(t, rec).Code; c != "invalid_params" {
		t.Errorf("code: got %q want invalid_params", c)
	}
}

func TestAddToken_NameTooLong_400(t *testing.T) {
	openTokenRestTestDB(t)
	longName := ""
	for i := 0; i < 60; i++ {
		longName += "x"
	}
	body := map[string]any{"name": longName, "unlimited_quota": true}
	ctx, rec := newRestContext(t, http.MethodPost, "/api/token/", body, nil, common.RoleCommonUser)
	AddToken(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status got %d want 400 body=%s", rec.Code, rec.Body.String())
	}
	if c := decodeRestError(t, rec).Code; c != "token_name_too_long" {
		t.Errorf("code: got %q want token_name_too_long", c)
	}
}

func TestAddToken_QuotaNegative_400(t *testing.T) {
	openTokenRestTestDB(t)
	body := map[string]any{"name": "t1", "unlimited_quota": false, "remain_quota": -10}
	ctx, rec := newRestContext(t, http.MethodPost, "/api/token/", body, nil, common.RoleCommonUser)
	AddToken(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status got %d want 400", rec.Code)
	}
	if c := decodeRestError(t, rec).Code; c != "token_quota_negative" {
		t.Errorf("code: got %q want token_quota_negative", c)
	}
}

func TestUpdateToken_NotFound_404(t *testing.T) {
	openTokenRestTestDB(t)
	body := map[string]any{"id": 9999, "name": "x", "unlimited_quota": true}
	ctx, rec := newRestContext(t, http.MethodPut, "/api/token/", body, nil, common.RoleCommonUser)
	UpdateToken(ctx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status got %d want 404 body=%s", rec.Code, rec.Body.String())
	}
	if c := decodeRestError(t, rec).Code; c != "token_not_found" {
		t.Errorf("code: got %q want token_not_found", c)
	}
}

func TestAddToken_HappyPath_201(t *testing.T) {
	openTokenRestTestDB(t)
	body := map[string]any{"name": "t-happy", "unlimited_quota": true}
	ctx, rec := newRestContext(t, http.MethodPost, "/api/token/", body, nil, common.RoleCommonUser)
	AddToken(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status got %d want 201 body=%s", rec.Code, rec.Body.String())
	}
}

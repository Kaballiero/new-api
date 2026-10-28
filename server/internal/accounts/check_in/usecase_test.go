package check_in

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	sqlite                bool
	checked               bool
	checkErr              error
	createErr, creditErr  error
	created, deleted      bool
	credited, transaction bool
}

func (s *fakeStore) Create(*domain.Checkin) error { s.created = true; return s.createErr }
func (s *fakeStore) Credit(int, int) error        { s.credited = true; return s.creditErr }
func (s *fakeStore) CheckedInOn(int, string) (bool, error) {
	return s.checked, s.checkErr
}
func (s *fakeStore) IsSQLite() bool               { return s.sqlite }
func (s *fakeStore) Delete(*domain.Checkin) error { s.deleted = true; return nil }
func (s *fakeStore) Transaction(run func(Mutation) error) error {
	s.transaction = true
	return run(s)
}
func (s *fakeStore) IncreaseCache(int, int64) error { return nil }

func TestCheckIn(t *testing.T) {
	storage := errors.New("timeout")
	enabled := &domain.CheckinPolicy{Enabled: true, MinQuota: 5, MaxQuota: 5}
	cases := []struct {
		name    string
		store   *fakeStore
		policy  *domain.CheckinPolicy
		err     error
		deleted bool
	}{
		{name: "disabled", store: &fakeStore{}, policy: &domain.CheckinPolicy{}, err: domain.ErrCheckinDisabled},
		{name: "lookup failure stays unclassified", store: &fakeStore{checkErr: storage}, policy: enabled, err: storage},
		{name: "already checked in", store: &fakeStore{checked: true}, policy: enabled, err: domain.ErrAlreadyCheckedIn},
		{name: "insert failure", store: &fakeStore{createErr: storage}, policy: enabled, err: domain.ErrCheckinFailed},
		{name: "credit failure", store: &fakeStore{creditErr: storage}, policy: enabled, err: domain.ErrCheckinQuotaFailed},
		{name: "sqlite credit failure removes the record", store: &fakeStore{sqlite: true, creditErr: storage}, policy: enabled, err: domain.ErrCheckinQuotaFailed, deleted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record, err := NewUsecase(tc.store, func() *domain.CheckinPolicy { return tc.policy }).Execute(7)
			require.ErrorIs(t, err, tc.err)
			assert.Nil(t, record)
			assert.Equal(t, tc.deleted, tc.store.deleted)
		})
	}
}

func TestCheckInAwardsQuota(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		store := &fakeStore{sqlite: sqlite}
		record, err := NewUsecase(store, func() *domain.CheckinPolicy { return &domain.CheckinPolicy{Enabled: true, MinQuota: 3, MaxQuota: 3} }).Execute(7)
		require.NoError(t, err)
		assert.Equal(t, 7, record.UserId)
		assert.Equal(t, 3, record.QuotaAwarded)
		assert.Equal(t, time.Now().Format("2006-01-02"), record.CheckinDate)
		assert.True(t, store.created)
		assert.True(t, store.credited)
		assert.Equal(t, !sqlite, store.transaction)
	}
}

func TestHandleRecordsLogInRequestLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	policy := func() *domain.CheckinPolicy { return &domain.CheckinPolicy{Enabled: true, MinQuota: 3, MaxQuota: 3} }
	var logged []string
	handler := NewHTTP(NewUsecase(&fakeStore{}, policy), policy, func(userID, quota int, lang string) {
		logged = append(logged, lang)
	})
	router := gin.New()
	router.POST("/checkin", func(c *gin.Context) { c.Set("id", 7) }, handler.Handle)
	request := httptest.NewRequest(http.MethodPost, "/checkin", nil)
	request.Header.Set("Accept-Language", "ru-RU")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, []string{i18n.LangRu}, logged)
}

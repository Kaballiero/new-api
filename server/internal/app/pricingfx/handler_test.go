package pricingfx_test

import (
	systemrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"

	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	systemstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/system"
	"github.com/QuantumNous/new-api/internal/app"
	pricingfx "github.com/QuantumNous/new-api/internal/app/pricingfx"
	"github.com/QuantumNous/new-api/internal/app/systemruntime"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func fxTaskHandler(t *testing.T) domain.SystemTaskHandler {
	for _, h := range scheduledTaskHandlers() {
		if h.Type() == domain.SystemTaskTypeModelCostFX {
			return h
		}
	}
	t.Fatal("FX handler missing from explicit assembly")
	return nil
}
func TestModelCostFXTaskRunSuccessAndFailure(t *testing.T) {
	pricingfx.ModelCostFXTestDialect(t)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   domain.SystemTaskStatus
	}{
		{"success", http.StatusOK, `{"Date":"%s","Valute":{"USD":{"CharCode":"USD","Value":92,"Nominal":1}}}`, systemrecord.SystemTaskStatusSucceeded},
		{"failure", http.StatusBadGateway, `{}`, systemrecord.SystemTaskStatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seed := pricingfx.ModelCostFXTestSeed(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				body := tc.body
				if strings.Contains(body, "%s") {
					body = fmt.Sprintf(body, time.Unix(seed.EffectiveAt+1, 0).UTC().Format(time.RFC3339))
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			pricingfx.SystemContractFXEndpoint(t, server.URL)
			task, err := systemStoreSystemTaskTest.CreateSystemTask(domain.SystemTaskTypeModelCostFX, nil, nil)
			require.NoError(t, err)
			claimed, ok, err := systemStoreSystemTaskTest.ClaimSystemTask(task.ID, domain.SystemTaskTypeModelCostFX, "fx-test", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, ok)
			fxTaskHandler(t).Run(context.Background(), claimed, "fx-test")
			latest, err := latestTask(domain.SystemTaskTypeModelCostFX)
			require.NoError(t, err)
			assert.Equal(t, tc.want, latest.Status)
			current, err := fxstate.CurrentModelCostFX(pricingdomainfx.ModelCostFXSource)
			require.NoError(t, err)
			wantRate := 90.0
			if tc.want == systemrecord.SystemTaskStatusSucceeded {
				wantRate = 92
			}
			assert.Equal(t, wantRate, current.Rates["USD"])
		})
	}
}

func TestModelCostFXTaskKeepsCommittedFXWhenFinishLosesLease(t *testing.T) {
	pricingfx.ModelCostFXTestDialect(t)
	pricingfx.ModelCostFXTestTruncate(t)
	_ = model.DB.Where("type = ?", domain.SystemTaskTypeModelCostFX).Delete(&model.SystemTask{})
	_ = model.DB.Where("type = ?", domain.SystemTaskTypeModelCostFX).Delete(&model.SystemTaskLock{})
	seed := pricingfx.ModelCostFXTestSeed(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(fmt.Sprintf(`{"Date":"%s","Valute":{"USD":{"CharCode":"USD","Value":93,"Nominal":1}}}`, time.Unix(seed.EffectiveAt+1, 0).UTC().Format(time.RFC3339))))
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	pricingfx.SystemContractFXEndpoint(t, server.URL)
	task, err := systemStoreSystemTaskTest.CreateSystemTask(domain.SystemTaskTypeModelCostFX, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := systemStoreSystemTaskTest.ClaimSystemTask(task.ID, domain.SystemTaskTypeModelCostFX, "fx-test", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	handler := fxTaskHandler(t)
	done := make(chan struct{})
	go func() { handler.Run(context.Background(), claimed, "fx-test"); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start HTTP")
	}
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("task_id = ?", task.TaskID).Update("locked_by", "other").Error)
	require.NoError(t, model.DB.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).Update("locked_by", "other").Error)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}
	current, err := fxstate.CurrentModelCostFX(pricingdomainfx.ModelCostFXSource)
	require.NoError(t, err)
	assert.Equal(t, 93.0, current.Rates["USD"])
	var latest model.SystemTask
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&latest).Error)
	assert.NotEqual(t, systemrecord.SystemTaskStatusSucceeded, latest.Status)
}

var systemStoreSystemTaskTest = systemstorage.Store{DB: func() *gorm.DB { return model.DB }}

func latestTask(kind string) (*domain.SystemTask, error) {
	rows, err := systemStoreSystemTaskTest.GetLatestSystemTasks([]string{kind})
	return rows[kind], err
}

func scheduledTaskHandlers() []domain.SystemTaskHandler {
	var handlers []domain.SystemTaskHandler
	systemruntime.RegisterScheduledSystemTasks(app.ChannelEffects{}, func(h domain.SystemTaskHandler) { handlers = append(handlers, h) })
	return handlers
}

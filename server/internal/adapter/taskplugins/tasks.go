package taskplugins

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/model"
)

type TaskReader struct{ Store taskstorage.Store }

func (r TaskReader) GetOwn(user int, id string) (*domain.Task, bool, error) {
	p, ok, e := r.Store.GetByTaskId(user, id)
	return record.ToDomain(p), ok, e
}
func (r TaskReader) ChannelEnabled(id int) (bool, error) {
	p, e := model.CacheGetChannel(id)
	return p != nil && p.Status == common.ChannelStatusEnabled, e
}
func (r TaskReader) ByPlatforms(user int, platforms, ids []string) ([]*domain.Task, error) {
	kinds := make([]constant.TaskPlatform, len(platforms))
	for i := range platforms {
		kinds[i] = constant.TaskPlatform(platforms[i])
	}
	rows, e := r.Store.GetByTaskIdsForPlatforms(user, kinds, ids)
	out := make([]*domain.Task, len(rows))
	for i := range rows {
		out[i] = record.ToDomain(rows[i])
	}
	return out, e
}

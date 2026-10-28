package system

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	"github.com/QuantumNous/new-api/internal/system/domain"
)

func taskValue(v *record.SystemTask) *domain.SystemTask {
	if v == nil {
		return nil
	}
	return &domain.SystemTask{
		ID:        v.ID,
		TaskID:    v.TaskID,
		Type:      v.Type,
		Status:    v.Status,
		ActiveKey: v.ActiveKey,
		Payload:   v.Payload,
		State:     v.State,
		Result:    v.Result,
		Error:     v.Error,
		LockedBy:  v.LockedBy,
		CreatedAt: v.CreatedAt,
		UpdatedAt: v.UpdatedAt,
	}
}
func (s Store) CreateSystemTask(kind string, payload, state any) (*domain.SystemTask, error) {
	v, e := s.createSystemTask(kind, payload, state)
	return taskValue(v), e
}
func (s Store) GetSystemTaskByTaskID(id string) (*domain.SystemTask, error) {
	v, e := s.getSystemTaskByTaskID(id)
	return taskValue(v), e
}
func (s Store) GetActiveSystemTask(kind string) (*domain.SystemTask, error) {
	v, e := s.getActiveSystemTask(kind)
	return taskValue(v), e
}
func (s Store) ListSystemTasks(limit int) ([]*domain.SystemTask, error) {
	v, e := s.listSystemTasks(limit)
	if e != nil {
		return nil, e
	}
	var out []*domain.SystemTask
	for _, t := range v {
		out = append(out, taskValue(t))
	}
	return out, nil
}
func (s Store) FindEarliestPendingSystemTasks(kinds []string) (map[string]*domain.SystemTask, error) {
	v, e := s.findEarliestPendingSystemTasks(kinds)
	if e != nil {
		return nil, e
	}
	out := map[string]*domain.SystemTask{}
	for k, t := range v {
		out[k] = taskValue(t)
	}
	return out, nil
}
func (s Store) GetLatestSystemTasks(kinds []string) (map[string]*domain.SystemTask, error) {
	v, e := s.getLatestSystemTasks(kinds)
	if e != nil {
		return nil, e
	}
	out := map[string]*domain.SystemTask{}
	for k, t := range v {
		out[k] = taskValue(t)
	}
	return out, nil
}
func (s Store) ClaimSystemTask(id int64, kind, runner string, until int64) (*domain.SystemTask, bool, error) {
	v, ok, e := s.claimSystemTask(id, kind, runner, until)
	return taskValue(v), ok, e
}
func (Store) Decode(data string, v any) error { return decodeSystemTaskJSONString(data, v) }
func (Store) JSONValue(data string) any       { return decodeSystemTaskJSONValue(data) }

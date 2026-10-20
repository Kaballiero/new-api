package authorization

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/authorization/initialize_authorization"
	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"sync"
)

const modelText = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act, eft

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.obj == p.obj && r.act == p.act && p.eft == "allow"
`

type State struct {
	mu    *sync.RWMutex
	value **casbin.SyncedEnforcer
}

func NewState(mu *sync.RWMutex, value **casbin.SyncedEnforcer) State {
	return State{mu: mu, value: value}
}
func (state State) Current() *casbin.SyncedEnforcer {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return *state.value
}
func (state State) Publish(e *casbin.SyncedEnforcer) {
	state.mu.Lock()
	*state.value = e
	state.mu.Unlock()
}
func (state State) Reload() error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if *state.value == nil {
		return fmt.Errorf("authz enforcer is not initialized")
	}
	return (*state.value).LoadPolicy()
}

type Policy struct{ enforcer *casbin.SyncedEnforcer }

func NewPolicy(e *casbin.SyncedEnforcer) *Policy {
	if e == nil {
		return nil
	}
	return &Policy{enforcer: e}
}
func (policy *Policy) GetFilteredPolicy(index int, values ...string) ([][]string, error) {
	return policy.enforcer.GetFilteredPolicy(index, values...)
}
func (policy *Policy) RemoveFilteredPolicy(index int, values ...string) (bool, error) {
	return policy.enforcer.RemoveFilteredPolicy(index, values...)
}
func (policy *Policy) AddPolicy(subject, resource, action, effect string) (bool, error) {
	return policy.enforcer.AddPolicy(subject, resource, action, effect)
}

type Engine struct {
	state   State
	adapter persist.Adapter
}

func NewEngine(state State, adapter persist.Adapter) Engine {
	return Engine{state: state, adapter: adapter}
}
func (engine Engine) CreateAndPublish() error {
	m, err := casbinmodel.NewModelFromString(modelText)
	if err != nil {
		return err
	}
	e, err := casbin.NewSyncedEnforcer(m, engine.adapter)
	if err != nil {
		return err
	}
	e.EnableAutoSave(true)
	engine.state.Publish(e)
	return nil
}
func (engine Engine) CurrentPolicy() initialize_authorization.Policy {
	p := NewPolicy(engine.state.Current())
	if p == nil {
		return nil
	}
	return p
}

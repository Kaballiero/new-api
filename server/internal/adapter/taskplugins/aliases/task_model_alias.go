package aliases

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

// TaskAliasTarget is one mapping-derived alias after cross-channel aggregation.
// Declared is empty for ambiguous tails within one plugin.
type TaskAliasTarget struct {
	Alias     string
	Declared  string
	PluginKey string
}

type taskAliasView struct {
	generation uint64
	expiresAt  time.Time
	byFold     map[string]TaskAliasTarget
}

const taskAliasViewTTL = 60 * time.Second

type View struct {
	read    func() ([]legacy.Channel, error)
	ptr     atomic.Pointer[taskAliasView]
	rebuild sync.Mutex
}

func NewView(read func() ([]legacy.Channel, error)) *View { return &View{read: read} }

// ResolveTaskModelAlias returns the mapping-derived alias target for name
// (exact or ASCII-folded). g is the caller's routing generation: a Number
// mismatch or TTL expiry rebuilds the view against that generation.
func (state *View) ResolveTaskModelAlias(g *jsplugin.RoutingGeneration, name string) (TaskAliasTarget, bool) {
	if g == nil || name == "" {
		return TaskAliasTarget{}, false
	}
	view := state.loadFreshTaskAliasView(g)
	if view == nil {
		return TaskAliasTarget{}, false
	}
	target, ok := view.byFold[jsplugin.ASCIIFold(name)]
	return target, ok
}

func (state *View) loadFreshTaskAliasView(g *jsplugin.RoutingGeneration) *taskAliasView {
	view := state.ptr.Load()
	if taskAliasViewFresh(view, g.Number) {
		return view
	}
	state.rebuild.Lock()
	defer state.rebuild.Unlock()
	view = state.ptr.Load()
	if taskAliasViewFresh(view, g.Number) {
		return view
	}
	rebuilt := state.buildTaskAliasView(g)
	state.ptr.Store(rebuilt)
	return rebuilt
}

func taskAliasViewFresh(view *taskAliasView, generation uint64) bool {
	return view != nil && view.generation == generation && time.Now().Before(view.expiresAt)
}

func (state *View) RebuildTaskAliasView() {
	state.rebuild.Lock()
	defer state.rebuild.Unlock()
	state.ptr.Store(state.buildTaskAliasView(jsplugin.DefaultRegistry.Generation()))
}

type taskAliasDraft struct {
	spellings []string
	byPlugin  map[string]map[string]struct{}
}

func (state *View) buildTaskAliasView(generation *jsplugin.RoutingGeneration) *taskAliasView {
	genNum := uint64(0)
	if generation != nil {
		genNum = generation.Number
	}
	view := &taskAliasView{
		generation: genNum,
		expiresAt:  time.Now().Add(taskAliasViewTTL),
		byFold:     make(map[string]TaskAliasTarget),
	}
	channels, err := state.read()
	if err != nil {
		common.SysError(fmt.Sprintf("rebuild task alias view: %s", err.Error()))
		return view
	}

	drafts := make(map[string]*taskAliasDraft)
	for i := range channels {
		channel := &channels[i]
		mappingJSON := channel.GetModelMapping()
		if mappingJSON == "" || mappingJSON == "{}" {
			continue
		}
		modelMap := make(map[string]string)
		if err := common.UnmarshalJsonStr(mappingJSON, &modelMap); err != nil {
			common.SysError(fmt.Sprintf("task alias view: channel %d model_mapping: %s", channel.Id, err.Error()))
			continue
		}
		inModels := make(map[string]struct{})
		for _, modelName := range channel.GetModels() {
			inModels[modelName] = struct{}{}
		}
		for alias, mapped := range modelMap {
			if mapped == "" {
				continue
			}
			if _, exposed := inModels[alias]; !exposed {
				continue
			}
			if _, declared := generation.CanonicalModel(alias); declared {
				continue
			}
			tail, cyclic := followChannelModelMapping(modelMap, alias)
			if cyclic {
				common.SysError(fmt.Sprintf("task alias mapping cycle dropped: channel=%d key=%q", channel.Id, alias))
				continue
			}
			declared, ok := generation.CanonicalModel(tail)
			if !ok {
				continue
			}
			plugin, ok := generation.GetByModel(declared)
			if !ok {
				continue
			}
			fold := jsplugin.ASCIIFold(alias)
			draft := drafts[fold]
			if draft == nil {
				draft = &taskAliasDraft{byPlugin: make(map[string]map[string]struct{})}
				drafts[fold] = draft
			}
			draft.spellings = append(draft.spellings, alias)
			declareds := draft.byPlugin[plugin.Meta.Key]
			if declareds == nil {
				declareds = make(map[string]struct{})
				draft.byPlugin[plugin.Meta.Key] = declareds
			}
			declareds[declared] = struct{}{}
		}
	}

	for _, draft := range drafts {
		alias := draft.spellings[0]
		for _, spelling := range draft.spellings[1:] {
			if spelling < alias {
				alias = spelling
			}
		}
		if len(draft.byPlugin) != 1 {
			pluginKeys := make([]string, 0, len(draft.byPlugin))
			for key := range draft.byPlugin {
				pluginKeys = append(pluginKeys, key)
			}
			common.SysLog(fmt.Sprintf("task model alias %q dropped: maps to multiple plugins %v", alias, pluginKeys))
			continue
		}
		var pluginKey, declared string
		for key, declareds := range draft.byPlugin {
			pluginKey = key
			if len(declareds) == 1 {
				for name := range declareds {
					declared = name
				}
			}
		}
		view.byFold[jsplugin.ASCIIFold(alias)] = TaskAliasTarget{
			Alias:     alias,
			Declared:  declared,
			PluginKey: pluginKey,
		}
	}
	return view
}

// followChannelModelMapping walks one channel's mapping the same way
// ModelMappedHelper does: visited-set cycle detection, self-map stops at
// the current hop, a non-self cycle is reported to the caller.
func followChannelModelMapping(modelMap map[string]string, start string) (string, bool) {
	return pricingdomain.FollowChannelModelMapping(modelMap, start)
}

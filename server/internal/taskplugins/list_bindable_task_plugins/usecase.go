package list_bindable_task_plugins

import (
	"context"
	"sort"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginVersion(string, string) (*domain.Plugin, error)
}
type Runtime interface {
	Get(string) (domain.Compiled, bool)
	Snapshot() domain.RegistrySnapshot
}
type Settings interface {
	FactoryDisabled(string) bool
	FactoryIcon(string) (string, []byte, bool)
}
type Usecase struct {
	store    Store
	runtime  Runtime
	settings Settings
}

func NewUsecase(store Store, runtime Runtime, settings Settings) Usecase {
	return Usecase{store: store, runtime: runtime, settings: settings}
}
func (u Usecase) Execute(ctx context.Context) (any, error) {
	snapshot := u.runtime.Snapshot()
	seen := make(map[string]bool)
	options := make([]map[string]any, 0, len(snapshot.Factory)+len(snapshot.Override))
	for layer, metas := range [][]domain.Meta{snapshot.Override, snapshot.Factory} {
		for _, meta := range metas {
			if seen[meta.Key] {
				continue
			}
			// Disabled factory keys are omitted from bind options. The disabled
			// set suppresses only the factory fallback; an enabled override for
			// the same key is listed in the override pass and still appears.
			if layer == 1 && u.settings.FactoryDisabled(meta.Key) {
				continue
			}
			if _, ok := u.runtime.Get(meta.Key); !ok {
				continue
			}
			seen[meta.Key] = true
			hasIcon := false
			if layer == 0 {
				if row, rowErr := u.store.GetTaskPluginVersion(meta.Key, ""); rowErr == nil {
					hasIcon = row.Icon != ""
				}
			} else {
				_, _, hasIcon = u.settings.FactoryIcon(meta.Key)
			}
			options = append(options, map[string]any{
				"key":          meta.Key,
				"name":         meta.Name,
				"icon":         meta.Icon,
				"hasIcon":      hasIcon,
				"baseUrl":      meta.BaseURL,
				"sortPriority": meta.SortPriority,
				"website":      meta.Website,
				"models":       meta.Models,
				"usageSchema":  meta.UsageSchema,
			})
		}
	}
	sort.Slice(options, func(i, j int) bool {
		left, right := options[i]["sortPriority"].(int), options[j]["sortPriority"].(int)
		if left != right {
			return left > right
		}
		return options[i]["key"].(string) < options[j]["key"].(string)
	})
	return options, nil
}

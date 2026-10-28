package app

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/create_prefill_group"
	"github.com/QuantumNous/new-api/internal/catalog/delete_prefill_group"
	"github.com/QuantumNous/new-api/internal/catalog/list_prefill_groups"
	"github.com/QuantumNous/new-api/internal/catalog/update_prefill_group"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

type PrefillHTTP struct {
	List   list_prefill_groups.HTTP
	Create create_prefill_group.HTTP
	Update update_prefill_group.HTTP
	Delete delete_prefill_group.HTTP
}

var prefillDefault = sync.OnceValue(newPrefillHTTP)

func PrefillHandlers() PrefillHTTP { return prefillDefault() }
func newPrefillHTTP() PrefillHTTP {
	groups := store.NewPrefillStore(func() *gorm.DB { return model.DB })
	return PrefillHTTP{List: list_prefill_groups.NewHTTP(groups), Create: create_prefill_group.NewHTTP(create_prefill_group.NewUsecase(groups, common.GetTimestamp)), Update: update_prefill_group.NewHTTP(update_prefill_group.NewUsecase(groups, common.GetTimestamp)), Delete: delete_prefill_group.NewHTTP(groups)}
}

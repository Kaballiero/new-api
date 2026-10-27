package app

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/http/identity"
	"github.com/QuantumNous/new-api/internal/adapter/identity/customoauth"
	"github.com/QuantumNous/new-api/internal/identity/create_custom_oauth_provider"
	"github.com/QuantumNous/new-api/internal/identity/delete_custom_oauth_provider"
	"github.com/QuantumNous/new-api/internal/identity/fetch_custom_oauth_discovery"
	"github.com/QuantumNous/new-api/internal/identity/get_custom_oauth_provider"
	"github.com/QuantumNous/new-api/internal/identity/list_custom_oauth_providers"
	"github.com/QuantumNous/new-api/internal/identity/update_custom_oauth_provider"
)

type CustomOAuthHTTP struct {
	Create    *create_custom_oauth_provider.HTTP
	Update    *update_custom_oauth_provider.HTTP
	List      *list_custom_oauth_providers.HTTP
	Get       *get_custom_oauth_provider.HTTP
	Delete    *delete_custom_oauth_provider.HTTP
	Discovery *fetch_custom_oauth_discovery.HTTP
}

func NewCustomOAuthHTTP() CustomOAuthHTTP {
	store, registry := customoauth.Store{}, customoauth.Registry{}
	return CustomOAuthHTTP{
		Create:    create_custom_oauth_provider.NewHTTP(create_custom_oauth_provider.NewUsecase(store, registry)),
		Update:    update_custom_oauth_provider.NewHTTP(update_custom_oauth_provider.NewUsecase(store, registry)),
		List:      list_custom_oauth_providers.NewHTTP(store),
		Get:       get_custom_oauth_provider.NewHTTP(get_custom_oauth_provider.NewUsecase(store)),
		Delete:    delete_custom_oauth_provider.NewHTTP(delete_custom_oauth_provider.NewUsecase(store, registry)),
		Discovery: fetch_custom_oauth_discovery.NewHTTP(fetch_custom_oauth_discovery.NewUsecase(identityhttp.Discovery{})),
	}
}

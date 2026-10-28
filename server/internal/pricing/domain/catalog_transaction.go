package domain

import "context"

type CatalogTransaction interface {
	UpdateHead(CatalogSource, string, string, string, int64) (bool, error)
	VisitDocumentIdentities(string, []string, func(string, string) error) error
	CreateDocument(CatalogDocument) error
	CreateRevision(CatalogRevision) error
}
type CatalogTransactions interface {
	Transaction(context.Context, func(CatalogTransaction) error) error
}

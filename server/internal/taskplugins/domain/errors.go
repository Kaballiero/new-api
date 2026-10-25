package domain

import "errors"

var (
	ErrSourceTooLarge  = errors.New("plugin source exceeds 1 MiB")
	ErrSourceHash      = errors.New("plugin source sha256 mismatch")
	ErrPluginNotFound  = errors.New("task plugin not found")
	ErrVersionNotFound = errors.New("plugin version not found")
	ErrDeleteFactory   = errors.New("override plugin version not found; factory plugins cannot be deleted")
	ErrMarketplaceName = errors.New("marketplace source name is required")
	ErrMarketplaceURL  = errors.New("marketplace source index_url must be an absolute http(s) URL")
	ErrSourceConflict  = errors.New("plugin key and version already exist with different source")
)

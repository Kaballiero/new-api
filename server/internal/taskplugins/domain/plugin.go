package domain

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type Plugin struct {
	Id         int64  `json:"id"`
	Key        string `json:"key"`
	APIVersion int    `json:"api_version"`
	Version    string `json:"version"`
	Source     string `json:"source"`
	SourceHash string `json:"source_hash"`
	// Icon is the plugin logo shipped as a sidecar icon.svg / icon.png next to
	// plugin.js, stored as a data URI so one column carries both the media
	// type and the bytes. It never travels inside list or detail JSON; the UI
	// loads it through GET /api/plugin/task/:key/icon. size matches the
	// 512 KiB icon cap used by plugin uploads.
	Icon      string `json:"-"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
	CreatedAt int64  `json:"created_at"`
	Remark    string `json:"remark"`
}

type ChannelRef struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// SyncSnapshot pairs enabled overrides with the revision of all active overrides.
type SyncSnapshot struct {
	Plugins  []Plugin
	Revision string
}

type DeleteResult struct {
	DeletedActive bool
	Promoted      *Plugin
}

type Meta struct {
	PriceCatalog  json.RawMessage             `json:"priceCatalog,omitempty"`
	SortPriority  int                         `json:"sortPriority,omitempty"`
	Website       string                      `json:"website,omitempty"`
	APIVersion    int                         `json:"apiVersion"`
	Key           string                      `json:"key"`
	Name          string                      `json:"name"`
	Icon          string                      `json:"icon,omitempty"`
	Description   json.RawMessage             `json:"description,omitempty"`
	Version       string                      `json:"version"`
	Author        AuthorMeta                  `json:"author"`
	BaseURL       string                      `json:"baseUrl,omitempty"`
	ChannelTypes  []int                       `json:"channelTypes,omitempty"`
	Models        []string                    `json:"models"`
	FetchMode     string                      `json:"fetchMode"`
	AllowedHosts  []string                    `json:"allowedHosts"`
	Routes        json.RawMessage             `json:"routes"`
	Protocols     json.RawMessage             `json:"protocols"`
	UsageSchema   map[string]UsageFieldSchema `json:"usageSchema,omitempty"`
	UsageExamples json.RawMessage             `json:"usageExamples,omitempty"`
	Auth          AuthMeta                    `json:"auth"`

	LegacyPriceBasis json.RawMessage `json:"legacyPriceBasis,omitempty"`
}

var ErrNotFound = errors.New("record not found")

type Options struct{ Key, Version string }
type Compiled interface{ Metadata() Meta }
type RegistrySnapshot struct{ Factory, Override []Meta }
type Generation struct {
	Number      uint64
	PublishedAt time.Time
}
type RoutingStatus struct {
	Generation  *Generation
	LastRebuild RebuildOutcome
	Errors      map[string]string
}
type Detail struct {
	Plugin  *Plugin `json:"plugin,omitempty"`
	Meta    Meta    `json:"meta"`
	Source  string  `json:"source"`
	Layer   string  `json:"layer"`
	HasIcon bool    `json:"has_icon"`
}
type Icon struct {
	MediaType string
	Data      []byte
}
type MarketplaceSource struct {
	Name     string `json:"name"`
	IndexURL string `json:"index_url"`
}
type InUseError struct {
	Channels []ChannelRef
	InFlight int64
}

func (e *InUseError) Error() string { return "task plugin is still in use" }

type SyncState struct {
	sync.Mutex
	Hashes      map[string]string
	Errors      map[string]string
	LastRebuild RebuildOutcome
}

type ListItem struct {
	Meta          Meta   `json:"meta"`
	Source        string `json:"source"`
	Enabled       bool   `json:"enabled"`
	Active        bool   `json:"active"`
	SourceHash    string `json:"source_hash"`
	HasIcon       bool   `json:"has_icon"`
	Remark        string `json:"remark"`
	RuntimeStatus string `json:"runtime_status"`
	RuntimeError  string `json:"runtime_error,omitempty"`
	FactoryMeta   *Meta  `json:"factory_meta,omitempty"`
	ChannelCount  int    `json:"channel_count"`
	InFlightCount int64  `json:"in_flight_count"`
}

type RebuildOutcome struct {
	Status           string    `json:"status"`
	AttemptedAt      time.Time `json:"attempted_at"`
	Generation       uint64    `json:"generation"`
	DatabaseRevision string    `json:"database_revision,omitempty"`
	PluginErrorCount int       `json:"plugin_error_count"`
	Error            string    `json:"error,omitempty"`
}

type RuntimeStatus struct {
	CurrentGeneration     uint64            `json:"current_generation"`
	GenerationPublishedAt time.Time         `json:"generation_published_at"`
	DatabaseRevision      string            `json:"database_revision"`
	DatabaseError         string            `json:"database_error,omitempty"`
	LastRebuild           RebuildOutcome    `json:"last_rebuild"`
	PluginErrors          map[string]string `json:"plugin_errors"`
}

func HasFactory(snapshot RegistrySnapshot, key string) bool {
	for _, meta := range snapshot.Factory {
		if meta.Key == key {
			return true
		}
	}
	return false
}

type AuthorMeta struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}
type AuthMeta struct {
	Type string `json:"type"`
}

type UsageFieldSchema struct {
	Type        string                       `json:"type,omitempty"`
	Unit        string                       `json:"unit,omitempty"`
	Enum        []string                     `json:"enum,omitempty"`
	Description map[string]string            `json:"description,omitempty"`
	EnumLabels  map[string]map[string]string `json:"enumLabels,omitempty"`
}

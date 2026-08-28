package services

import (
	"reflect"

	"github.com/scrypster/huginn/internal/config"
)

// ConfigService is the typed interface for reading and persisting configuration.
type ConfigService interface {
	Get() config.Config
	Save(cfg config.Config) error
}

// DirectConfigService implements ConfigService using a *config.Config pointer.
type DirectConfigService struct {
	cfg *config.Config
}

// NewDirectConfigService wraps a *config.Config.
func NewDirectConfigService(cfg *config.Config) ConfigService {
	return &DirectConfigService{cfg: cfg}
}

func (s *DirectConfigService) Get() config.Config {
	return *s.cfg
}

// Save persists cfg. It deliberately does NOT call cfg.Save() (a whole-struct
// overwrite of cfg's snapshot): the caller's cfg was produced from an earlier
// Get(), which may be stale relative to disk by the time Save is called (the
// config API, or another writer, may have saved unrelated fields since). A
// naive cfg.Save() would revert those fields. Instead, diff cfg against the
// live s.cfg this service was constructed with and apply only the fields
// that actually changed on top of whatever is on disk right now.
func (s *DirectConfigService) Save(cfg config.Config) error {
	before := *s.cfg
	return config.UpdateDefault(func(disk *config.Config) {
		mergeChangedFields(disk, before, cfg)
	})
}

// mergeChangedFields copies each field of after onto disk where it differs
// from before — i.e. only the fields the caller actually changed relative to
// the snapshot it started from — leaving every other field of disk (which may
// hold changes from another writer) untouched.
//
// The walk recurses into nested structs (backend.*, web_ui.*, integrations.*,
// cloud.*) rather than stopping at top-level granularity. Top-level-only
// granularity would reproduce the very clobber this merge exists to prevent,
// one level down: a caller changing only backend.provider would assign its
// whole stale BackendConfig, reverting a backend.api_key another writer (e.g.
// main.go's relay UpdateModelConfig closure) had just saved. Slices, maps and
// scalars are compared and assigned whole — there is no meaningful per-element
// merge for them.
func mergeChangedFields(disk *config.Config, before, after config.Config) {
	mergeChangedValue(reflect.ValueOf(disk).Elem(), reflect.ValueOf(before), reflect.ValueOf(after))
}

func mergeChangedValue(dv, bv, av reflect.Value) {
	t := dv.Type()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).PkgPath != "" {
			continue // unexported; unreadable and unsettable via reflection
		}
		df, bf, af := dv.Field(i), bv.Field(i), av.Field(i)
		if reflect.DeepEqual(bf.Interface(), af.Interface()) {
			continue
		}
		if df.Kind() == reflect.Struct && allFieldsExported(df.Type()) {
			mergeChangedValue(df, bf, af)
			continue
		}
		df.Set(af)
	}
}

// allFieldsExported reports whether every immediate field of t is exported,
// so mergeChangedValue can recurse into it without leaving unexported state
// behind. A struct carrying unexported fields is assigned whole instead.
func allFieldsExported(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).PkgPath != "" {
			return false
		}
	}
	return true
}

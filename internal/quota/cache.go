package quota

import (
	"github.com/YukiagoTpf/muster/internal/store"
)

const cacheFile = "quota.json"

type cache struct {
	Version int      `json:"version"`
	Reports []Report `json:"reports"`
}

// SaveCache stores the latest reports, for views that must not wait on the
// network (the picker). The caller holds the store lock.
func SaveCache(s *store.Store, reports []Report) error {
	return store.WriteJSON(s.Path(cacheFile), cache{Version: 1, Reports: reports})
}

// LoadCache returns the cached reports; none when there is no usable cache.
func LoadCache(s *store.Store) []Report {
	var c cache
	if _, err := store.ReadJSON(s.Path(cacheFile), &c); err != nil || c.Version != 1 {
		return nil
	}
	return c.Reports
}

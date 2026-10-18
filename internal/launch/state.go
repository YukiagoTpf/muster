package launch

import (
	"github.com/YukiagoTpf/muster/internal/store"
)

// Last is what the picker offers first: the most recently launched credential
// and, per credential, the model last launched with it.
type Last struct {
	Version    int               `json:"version"`
	Credential string            `json:"credential,omitempty"`
	Models     map[string]string `json:"models,omitempty"`
}

const stateFile = "state.json"

// LoadLast reads the last-launch state; a missing or unreadable file is empty
// state — it only orders the picker.
func LoadLast(s *store.Store) Last {
	var l Last
	if _, err := store.ReadJSON(s.Path(stateFile), &l); err != nil || l.Version != 1 {
		return Last{Version: 1}
	}
	return l
}

// Remember records a launch as the last one.
func Remember(s *store.Store, cred, model string) error {
	return s.WithLock(func() error {
		l := LoadLast(s)
		l.Credential = cred
		if l.Models == nil {
			l.Models = map[string]string{}
		}
		l.Models[cred] = model
		return store.WriteJSON(s.Path(stateFile), l)
	})
}

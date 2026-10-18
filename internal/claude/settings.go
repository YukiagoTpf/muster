package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// member is one key of a JSON object, its value kept as written.
type member struct {
	key   string
	value json.RawMessage
}

// SetProviderEnv rewrites a settings file so its provider env is exactly env:
// every provider key in "env" is removed and env's keys are written where the
// first of them stood (at the start of "env" if there was none). Every other
// key — top level or in "env" — keeps its value and its place; the file is
// re-indented with two spaces. Empty input is an empty settings file.
func SetProviderEnv(settings []byte, env map[string]string) ([]byte, error) {
	if len(bytes.TrimSpace(settings)) == 0 {
		settings = []byte("{}")
	}
	top, err := members(settings)
	if err != nil {
		return nil, fmt.Errorf("settings file: %w", err)
	}
	envAt := -1
	var envMembers []member
	for i, m := range top {
		if m.key == "env" {
			if envMembers, err = members(m.value); err != nil {
				return nil, fmt.Errorf(`settings file "env": %w`, err)
			}
			envAt = i
		}
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var fresh []member
	for _, k := range keys {
		v, err := marshal(env[k])
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, member{k, v})
	}

	var out []member
	inserted := false
	for _, m := range envMembers {
		if !IsProviderEnvKey(m.key) {
			out = append(out, m)
			continue
		}
		if !inserted {
			out, inserted = append(out, fresh...), true
		}
	}
	if !inserted {
		out = append(fresh, out...)
	}
	envValue := object(out)
	if envAt >= 0 {
		top[envAt].value = envValue
	} else if len(fresh) > 0 {
		top = append(top, member{"env", envValue})
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, object(top), "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// SharedSettings is a settings file without its provider env — the part every
// instance and every machine of the platform shares. "env" stays where it
// stood, even if nothing is left in it, so the provider env goes back in the
// same place.
func SharedSettings(settings []byte) ([]byte, error) { return SetProviderEnv(settings, nil) }

// members splits a JSON object into its members, in order, values verbatim.
func members(b []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var out []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, member{tok.(string), v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("trailing data after the object")
	}
	return out, nil
}

// object joins members back into a compact JSON object.
func object(ms []member) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// marshal encodes a string without HTML escaping, so "<" stays "<".
func marshal(s string) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

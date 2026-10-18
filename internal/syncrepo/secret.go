package syncrepo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/YukiagoTpf/muster/internal/credential"
)

// The repo's secrets are age files, so they stay readable with the age tool
// alone. Each credential is encrypted to the repo's identity — a hybrid
// (post-quantum) age key generated when the repo was created — and the
// identity itself is encrypted with the user's passphrase (scrypt). A machine
// asks for the passphrase once, when it joins, and keeps the identity in its
// sync config, as it keeps credential keys in secrets.json: in a file only the
// user can read.

// ScryptWorkFactor is the scrypt cost of the passphrase (age's default, about
// a second). Tests lower it.
var ScryptWorkFactor = 18

// ErrWrongPassphrase reports a passphrase that does not open the repo's identity.
var ErrWrongPassphrase = errors.New("wrong passphrase")

func newIdentity() (*age.HybridIdentity, error) { return age.GenerateHybridIdentity() }

func parseIdentity(s string) (*age.HybridIdentity, error) { return age.ParseHybridIdentity(s) }

// sealIdentity encrypts the identity with the passphrase.
func sealIdentity(id *age.HybridIdentity, passphrase string) ([]byte, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	r.SetWorkFactor(ScryptWorkFactor)
	return seal([]byte(id.String()+"\n"), r)
}

// openIdentity decrypts the identity with the passphrase.
func openIdentity(sealed []byte, passphrase string) (*age.HybridIdentity, error) {
	i, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	plain, err := open(sealed, i)
	if errors.Is(err, age.ErrIncorrectIdentity) {
		return nil, ErrWrongPassphrase
	}
	if err != nil {
		return nil, err
	}
	return parseIdentity(strings.TrimSpace(string(plain)))
}

// entryFile is a credential file's plaintext.
type entryFile struct {
	Version int `json:"version"`
	credential.Entry
}

// entryBytes is an entry's plaintext. Two entries are the same credential
// exactly when these bytes are equal.
func entryBytes(e credential.Entry) []byte {
	b, _ := json.Marshal(entryFile{1, e})
	return b
}

func sealEntry(e credential.Entry, id *age.HybridIdentity) ([]byte, error) {
	return seal(entryBytes(e), id.Recipient())
}

func openEntry(sealed []byte, id *age.HybridIdentity) (credential.Entry, error) {
	plain, err := open(sealed, id)
	if err != nil {
		return credential.Entry{}, err
	}
	var f entryFile
	if err := json.Unmarshal(plain, &f); err != nil {
		return credential.Entry{}, err
	}
	if f.Version != 1 {
		return credential.Entry{}, fmt.Errorf("version %d; this muster understands 1", f.Version)
	}
	return f.Entry, nil
}

func seal(plain []byte, r age.Recipient) ([]byte, error) {
	var buf bytes.Buffer
	a := armor.NewWriter(&buf)
	w, err := age.Encrypt(a, r)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := a.Close(); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func open(sealed []byte, id age.Identity) ([]byte, error) {
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(sealed)), id)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

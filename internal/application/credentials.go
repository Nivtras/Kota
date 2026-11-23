package application

import (
	"errors"
	"strings"
)

// KeyDef describes a provider that needs a key the user enters.
type KeyDef struct {
	ID     string // provider ID
	Name   string
	Secret string // name in the SecretStore
	Hint   string // where to get one
	URL    string // page to create one
}

// Credential is a KeyDef plus whether a key is stored. The key itself never
// leaves the store.
type Credential struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Set  bool   `json:"set"`
	Hint string `json:"hint"`
	URL  string `json:"url"`
}

// CredentialService manages the user's API keys.
type CredentialService struct {
	store SecretStore
	defs  []KeyDef
}

func NewCredentialService(store SecretStore, defs []KeyDef) *CredentialService {
	return &CredentialService{store: store, defs: defs}
}

// List is every key-based provider and whether its key is set.
func (c *CredentialService) List() []Credential {
	out := make([]Credential, 0, len(c.defs))
	for _, d := range c.defs {
		out = append(out, Credential{ID: d.ID, Name: d.Name, Set: c.store.Get(d.Secret) != "", Hint: d.Hint, URL: d.URL})
	}
	return out
}

func (c *CredentialService) def(id string) (KeyDef, bool) {
	for _, d := range c.defs {
		if d.ID == id {
			return d, true
		}
	}
	return KeyDef{}, false
}

// Set stores a key (surrounding spaces and quotes removed).
func (c *CredentialService) Set(id, key string) error {
	d, ok := c.def(id)
	if !ok {
		return errors.New("unknown provider")
	}
	key = strings.Trim(strings.TrimSpace(key), `"'`)
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return errors.New("that doesn't look like a key")
	}
	return c.store.Set(d.Secret, key)
}

// Clear removes a stored key.
func (c *CredentialService) Clear(id string) error {
	d, ok := c.def(id)
	if !ok {
		return errors.New("unknown provider")
	}
	return c.store.Set(d.Secret, "")
}

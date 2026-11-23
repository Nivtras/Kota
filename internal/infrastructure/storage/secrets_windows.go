//go:build windows

package storage

// API keys the user enters in Kota (OpenRouter, GitHub). They are kept in
// secrets.dat, encrypted with DPAPI for the current Windows user: only this
// user on this machine can decrypt them. They never appear in config.json
// or in logs.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const secretsFile = "secrets.dat"

var secretsMu sync.Mutex

func secretsEntropy() *windows.DataBlob {
	e := []byte("Kota secrets v1")
	return &windows.DataBlob{Size: uint32(len(e)), Data: &e[0]}
}

func loadSecrets() map[string]string {
	m := map[string]string{}
	enc, err := os.ReadFile(Path(secretsFile))
	if err != nil || len(enc) == 0 {
		return m
	}
	in := windows.DataBlob{Size: uint32(len(enc)), Data: &enc[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, secretsEntropy(), 0, nil, 0, &out); err != nil {
		return m
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	plain := unsafe.Slice(out.Data, out.Size)
	_ = json.Unmarshal(plain, &m)
	return m
}

func saveSecrets(m map[string]string) error {
	plain, err := json.Marshal(m)
	if err != nil {
		return err
	}
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, windows.StringToUTF16Ptr("Kota"), secretsEntropy(), 0, nil, 0, &out); err != nil {
		return err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	enc := append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
	p := Path(secretsFile)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, enc, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Secret returns a stored secret ("" if none).
func Secret(name string) string {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	return loadSecrets()[name]
}

// SetSecret stores a secret; an empty value removes it.
func SetSecret(name, value string) error {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	m := loadSecrets()
	if value == "" {
		delete(m, name)
	} else {
		m[name] = value
	}
	return saveSecrets(m)
}

// Secrets is the stored secrets as an object (application.SecretStore).
type Secrets struct{}

func (Secrets) Get(name string) string       { return Secret(name) }
func (Secrets) Set(name, value string) error { return SetSecret(name, value) }

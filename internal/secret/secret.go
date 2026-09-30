// Package secret protects stored credentials: DPAPI on Windows, AES-GCM with a key kept
// in the Secret Service keyring (GNOME Keyring/KWallet) on Linux. Elsewhere values are
// only base64-encoded so tests can run.
package secret

import "encoding/base64"

// Encrypt returns an opaque string safe to persist.
func Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	b, err := protect([]byte(plain))
	if err != nil {
		return "", err
	}
	return prefix + base64.StdEncoding.EncodeToString(b), nil
}

// Decrypt reverses Encrypt. Values without the platform prefix are returned as-is.
func Decrypt(stored string) (string, error) {
	if len(stored) < len(prefix) || stored[:len(prefix)] != prefix {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(prefix):])
	if err != nil {
		return "", err
	}
	b, err := unprotect(raw)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

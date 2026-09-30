//go:build linux

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
)

const (
	prefix         = "aesgcm:"
	keyringService = appinfo.Name
	keyringUser    = "master-key"
)

var (
	keyOnce sync.Once
	key     []byte
	keyErr  error

	// keyFileDir, when set, skips the keyring and keeps the key file there (tests).
	keyFileDir string
)

// UseKeyFileIn makes this process keep its key in dir instead of the keyring.
// It must be called before the first Encrypt/Decrypt.
func UseKeyFileIn(dir string) { keyFileDir = dir }

// masterKey loads (or creates) the 256-bit key: from the desktop keyring when one is
// running, otherwise from a user-only file in the config directory.
func masterKey() ([]byte, error) {
	keyOnce.Do(func() {
		if keyFileDir != "" {
			key, keyErr = fileKey()
			return
		}
		if s, err := keyring.Get(keyringService, keyringUser); err == nil {
			key, keyErr = base64.StdEncoding.DecodeString(s)
			return
		} else if errors.Is(err, keyring.ErrNotFound) {
			k := make([]byte, 32)
			if _, err := rand.Read(k); err != nil {
				keyErr = err
				return
			}
			if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(k)); err == nil {
				key = k
				return
			}
		}
		log.Print("secret: no keyring available; using a user-only key file")
		key, keyErr = fileKey()
	})
	return key, keyErr
}

func fileKey() ([]byte, error) {
	dir := keyFileDir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, appinfo.Name)
	}
	path := filepath.Join(dir, ".secret-key")
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return k, os.WriteFile(path, k, 0o600)
}

func gcm() (cipher.AEAD, error) {
	k, err := masterKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func protect(data []byte) ([]byte, error) {
	g, err := gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, data, nil), nil
}

func unprotect(data []byte) ([]byte, error) {
	g, err := gcm()
	if err != nil {
		return nil, err
	}
	if len(data) < g.NonceSize() {
		return nil, fmt.Errorf("secret: ciphertext too short")
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], nil)
}

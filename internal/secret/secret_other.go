//go:build !windows && !linux

package secret

const prefix = "plain:"

func protect(data []byte) ([]byte, error)   { return data, nil }
func unprotect(data []byte) ([]byte, error) { return data, nil }

// UseKeyFileIn is a no-op on platforms without key management.
func UseKeyFileIn(string) {}

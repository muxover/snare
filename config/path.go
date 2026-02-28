package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultStoreDir = ".snare/captures"
	DefaultCADir    = ".snare"
)

// StoreDir returns the directory for persisted captures.
func StoreDir() string {
	if d := os.Getenv("SNARE_STORE"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, DefaultStoreDir)
	}
	return DefaultStoreDir
}

// CADir returns the directory for CA certs.
func CADir() string {
	if d := os.Getenv("SNARE_CA"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, DefaultCADir)
	}
	return DefaultCADir
}

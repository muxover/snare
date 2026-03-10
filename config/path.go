package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultStoreDir = ".snare/captures"
	DefaultCADir    = ".snare"
)

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

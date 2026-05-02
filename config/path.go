package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultStoreDir = ".snare/captures"
	DefaultCADir    = ".snare"
	DefaultMockFile = ".snare/mocks.json"
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

func MockFile() string {
	if f := os.Getenv("SNARE_MOCKS"); f != "" {
		return f
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, DefaultMockFile)
	}
	return DefaultMockFile
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

package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type FileConfig struct {
	Port             string   `yaml:"port"`
	Bind             string   `yaml:"bind"`
	NoMITM           bool     `yaml:"no_mitm"`
	Verbose          bool     `yaml:"verbose"`
	MaxCaptures      int      `yaml:"max_captures"`
	UpstreamProxy    string   `yaml:"upstream_proxy"`
	RewriteHost      []string `yaml:"rewrite_host"`
	AddHeader        []string `yaml:"add_header"`
	RemoveHeader     []string `yaml:"remove_header"`
	MockFile         string   `yaml:"mock_file"`
	Intercept        string   `yaml:"intercept"`
	InterceptTimeout string   `yaml:"intercept_timeout"`
	OnCapture        string   `yaml:"on_capture"`
	Mode             string   `yaml:"mode"`
	Target           string   `yaml:"target"`
	Ignore           []string `yaml:"ignore"`
	MapRemote        []string `yaml:"map_remote"`
	RewriteBody      []string `yaml:"rewrite_body"`
	MaxBodySize      int64    `yaml:"max_body_size"`
	Delay            string   `yaml:"delay"`
	Chaos            float64  `yaml:"chaos"`
	Shadow           []string `yaml:"shadow"`
	Plugins          []string `yaml:"plugins"`
	Web              bool     `yaml:"web"`
	WebPort          string   `yaml:"web_port"`
}

func ConfigFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".snare", "config.yaml")
}

func LoadFileConfig() (*FileConfig, error) {
	data, err := os.ReadFile(ConfigFilePath())
	if os.IsNotExist(err) {
		return &FileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg FileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

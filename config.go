package main

import (
	"log"
	"os"
	"strconv"

	"gopkg.in/yaml.v2"
)

type ServerConfig struct {
	Port                 int `yaml:"port"`
	TokenExpirationHours int `yaml:"token_expiration_hours"`
}

type Config struct {
	Server ServerConfig `yaml:"server"`
}

var config *Config

func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func getPort() string {
	if config == nil || config.Server.Port == 0 {
		return ":8000"
	}
	return ":" + strconv.Itoa(config.Server.Port)
}

func init() {
	var err error
	config, err = loadConfig("config.yaml")
	if err != nil {
		log.Printf("Warning: Could not load config.yaml, using defaults: %v\n", err)
		config = &Config{
			Server: ServerConfig{Port: 8000, TokenExpirationHours: 24},
		}
	}
	// Set default token expiration if not configured
	if config.Server.TokenExpirationHours == 0 {
		config.Server.TokenExpirationHours = 24
	}
}

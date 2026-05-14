package main

import (
	"log"
	"os"
	"strconv"

	"gopkg.in/yaml.v2"
)

type ServerConfig struct {
	Port                 int      `yaml:"port"`
	TokenExpirationHours int      `yaml:"token_expiration_hours"`
	AdminAllowedIPs      []string `yaml:"admin_allowed_ips"`
	RateLimit            int      `yaml:"rate_limit"`
	MaxBodyBytes         int64    `yaml:"max_body_bytes"`
	ReadTimeoutSecs      int      `yaml:"read_timeout_secs"`
	WriteTimeoutSecs     int      `yaml:"write_timeout_secs"`
	IdleTimeoutSecs      int      `yaml:"idle_timeout_secs"`
	MaxConnsPerIP        int      `yaml:"max_conns_per_ip"`
	ImagesDir            string   `yaml:"images_dir"`
	ImageXMax            int      `yaml:"image_x_max"`
	ImageYMax            int      `yaml:"image_y_max"`
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
	if config.Server.TokenExpirationHours == 0 {
		config.Server.TokenExpirationHours = 24
	}
	if config.Server.MaxBodyBytes == 0 {
		config.Server.MaxBodyBytes = 1 << 20 // 1 MiB
	}
	if config.Server.ReadTimeoutSecs == 0 {
		config.Server.ReadTimeoutSecs = 10
	}
	if config.Server.WriteTimeoutSecs == 0 {
		config.Server.WriteTimeoutSecs = 30
	}
	if config.Server.IdleTimeoutSecs == 0 {
		config.Server.IdleTimeoutSecs = 60
	}
	if config.Server.MaxConnsPerIP == 0 {
		config.Server.MaxConnsPerIP = 10
	}
	if config.Server.ImagesDir == "" {
		config.Server.ImagesDir = "./images"
	}
	if config.Server.ImageXMax == 0 {
		config.Server.ImageXMax = 1024
	}
	if config.Server.ImageYMax == 0 {
		config.Server.ImageYMax = 1024
	}
}

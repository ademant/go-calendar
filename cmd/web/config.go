package main

import (
	"flag"
	"log"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Listen    string `yaml:"listen"`
	Domain    string `yaml:"domain"`
	DansalURL string `yaml:"dansal_url"`
	DBPath    string `yaml:"db_path"`
	PollSecs  int    `yaml:"poll_secs"`
	I18nFile  string `yaml:"i18n_file"` // optional path to override embedded i18n.yaml
}

func loadConfig() *Config {
	cfg := &Config{
		Listen:   ":8080",
		DBPath:   "web.db",
		PollSecs: 300,
	}

	configPath := ""
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()
	if configPath == "" && flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			log.Fatalf("read config: %v", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			log.Fatalf("parse config: %v", err)
		}
	}

	if v := os.Getenv("DANSAL_DOMAIN"); v != "" {
		cfg.Domain = v
	}
	if v := os.Getenv("DANSAL_URL"); v != "" {
		cfg.DansalURL = v
	}

	if cfg.Domain == "" {
		log.Fatal("domain is required (set via config file or DANSAL_DOMAIN env var)")
	}
	if cfg.DansalURL == "" {
		log.Fatal("dansal_url is required (set via config file or DANSAL_URL env var)")
	}

	return cfg
}

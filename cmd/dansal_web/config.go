package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Listen    string `yaml:"listen"`
	Domain    string `yaml:"domain"`
	BaseURL   string `yaml:"base_url"`   // optional; defaults to https://{domain}
	DansalURL string `yaml:"dansal_url"`
	DBPath    string `yaml:"db_path"`
	PollSecs  int    `yaml:"poll_secs"`
	I18nFile  string `yaml:"i18n_file"`  // optional path to override embedded i18n.yaml
	PagesFile string `yaml:"pages_file"` // optional path to impressum/contact YAML

	// NodeInfo metadata (served at /nodeinfo/2.1)
	NodeInfoDescription     string `yaml:"nodeinfo_description"`
	NodeInfoMaintainerName  string `yaml:"nodeinfo_maintainer_name"`
	NodeInfoMaintainerEmail string `yaml:"nodeinfo_maintainer_email"`

	// Federation
	ShowFederatedEvents bool `yaml:"show_federated_events"`

	// Layout
	ImagesDir        string `yaml:"images_dir"`         // directory for logo.svg, banner.svg, favicon.svg
	BannerHeightMain int    `yaml:"banner_height_main"` // px; 0 = hidden
	BannerHeightSub  int    `yaml:"banner_height_sub"`  // px; 0 = hidden (default)
	LogoHeightMain   int    `yaml:"logo_height_main"`   // px in nav on main page
	LogoHeightSub    int    `yaml:"logo_height_sub"`    // px in nav on sub pages
	DarkMode         string `yaml:"dark_mode"`          // "auto" (default), "light", "dark"

	pagesContent *PagesContent

	// Loaded from web.db at startup; overridden via admin site-config page.
	SiteName       string
	ContactOverride string
}

// publicBaseURL returns the canonical public URL of the web app.
func (cfg *Config) publicBaseURL() string {
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	return "https://" + cfg.Domain
}

func loadConfig() *Config {
	cfg := &Config{
		Listen:           ":8080",
		DBPath:           "web.db",
		PollSecs:         300,
		BannerHeightMain: 200,
		BannerHeightSub:  0,
		LogoHeightMain:   48,
		LogoHeightSub:    32,
		DarkMode:         "auto",
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

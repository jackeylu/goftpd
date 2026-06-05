package main

import (
	"fmt"
	"os"

	"gopkg.in/ini.v1"
)

// Config maps to config.ini sections.
type Config struct {
	Auth struct {
		Enabled  bool
		Username string
		Password string
	}
	Permissions Permissions
	Network     struct {
		Port           int
		MaxConnections int
	}
	Storage struct {
		SharedDir string
	}
}

// LoadConfig reads an INI file and returns a populated Config.
func LoadConfig(path string) (*Config, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	c := &Config{}

	// [auth]
	auth := cfg.Section("auth")
	c.Auth.Enabled = auth.Key("enabled").MustBool(true)
	c.Auth.Username = auth.Key("username").MustString("anonymous")
	c.Auth.Password = auth.Key("password").MustString("")

	// [permissions]
	perm := cfg.Section("permissions")
	c.Permissions.Download = perm.Key("download").MustBool(true)
	c.Permissions.Upload = perm.Key("upload").MustBool(true)
	c.Permissions.Delete = perm.Key("delete").MustBool(true)
	c.Permissions.Rename = perm.Key("rename").MustBool(true)
	c.Permissions.Mkdir = perm.Key("mkdir").MustBool(true)
	c.Permissions.MaxUploadFileSize = perm.Key("max_upload_file_size").MustInt64(0)
	c.Permissions.MaxIPFiles = perm.Key("max_ip_files").MustInt(0)

	// [network]
	net := cfg.Section("network")
	c.Network.Port = net.Key("port").MustInt(21)
	c.Network.MaxConnections = net.Key("max_connections").MustInt(100)

	// [storage]
	store := cfg.Section("storage")
	c.Storage.SharedDir = store.Key("shared_dir").MustString(".")
	if c.Storage.SharedDir == "" {
		c.Storage.SharedDir, _ = os.Getwd()
	}

	return c, nil
}

// SaveConfig writes a Config to an INI file.
func SaveConfig(path string, c *Config) error {
	cfg := ini.Empty()

	auth := cfg.Section("auth")
	auth.Key("enabled").SetValue(boolToStr(c.Auth.Enabled))
	auth.Key("username").SetValue(c.Auth.Username)
	auth.Key("password").SetValue(c.Auth.Password)

	perm := cfg.Section("permissions")
	perm.Key("download").SetValue(boolToStr(c.Permissions.Download))
	perm.Key("upload").SetValue(boolToStr(c.Permissions.Upload))
	perm.Key("delete").SetValue(boolToStr(c.Permissions.Delete))
	perm.Key("rename").SetValue(boolToStr(c.Permissions.Rename))
	perm.Key("mkdir").SetValue(boolToStr(c.Permissions.Mkdir))
	perm.Key("max_upload_file_size").SetValue(fmt.Sprintf("%d", c.Permissions.MaxUploadFileSize))
	perm.Key("max_ip_files").SetValue(fmt.Sprintf("%d", c.Permissions.MaxIPFiles))

	net := cfg.Section("network")
	net.Key("port").SetValue(fmt.Sprintf("%d", c.Network.Port))
	net.Key("max_connections").SetValue(fmt.Sprintf("%d", c.Network.MaxConnections))

	store := cfg.Section("storage")
	store.Key("shared_dir").SetValue(c.Storage.SharedDir)

	return cfg.SaveTo(path)
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

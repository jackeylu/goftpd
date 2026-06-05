package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ini")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_AllFields(t *testing.T) {
	path := writeTestConfig(t, `
[auth]
enabled = true
username = admin
password = secret

[permissions]
download = true
upload = true
delete = false
rename = true
mkdir = false

[network]
port = 2121
max_connections = 50

[storage]
shared_dir = /tmp/ftp
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !cfg.Auth.Enabled {
		t.Error("Auth.Enabled should be true")
	}
	if cfg.Auth.Username != "admin" {
		t.Errorf("Username: got %q, want %q", cfg.Auth.Username, "admin")
	}
	if cfg.Auth.Password != "secret" {
		t.Errorf("Password: got %q, want %q", cfg.Auth.Password, "secret")
	}

	if !cfg.Permissions.Download {
		t.Error("Download should be true")
	}
	if !cfg.Permissions.Upload {
		t.Error("Upload should be true")
	}
	if cfg.Permissions.Delete {
		t.Error("Delete should be false")
	}
	if !cfg.Permissions.Rename {
		t.Error("Rename should be true")
	}
	if cfg.Permissions.Mkdir {
		t.Error("Mkdir should be false")
	}

	if cfg.Network.Port != 2121 {
		t.Errorf("Port: got %d, want 2121", cfg.Network.Port)
	}
	if cfg.Network.MaxConnections != 50 {
		t.Errorf("MaxConnections: got %d, want 50", cfg.Network.MaxConnections)
	}

	if cfg.Storage.SharedDir != "/tmp/ftp" {
		t.Errorf("SharedDir: got %q, want %q", cfg.Storage.SharedDir, "/tmp/ftp")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	path := writeTestConfig(t, ``) // empty file, all defaults

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !cfg.Auth.Enabled {
		t.Error("Default Auth.Enabled should be true")
	}
	if cfg.Auth.Username != "anonymous" {
		t.Errorf("Default Username: got %q, want %q", cfg.Auth.Username, "anonymous")
	}
	if cfg.Network.Port != 21 {
		t.Errorf("Default Port: got %d, want 21", cfg.Network.Port)
	}
	if cfg.Network.MaxConnections != 100 {
		t.Errorf("Default MaxConnections: got %d, want 100", cfg.Network.MaxConnections)
	}
	if cfg.Storage.SharedDir != "." {
		t.Errorf("Default SharedDir: got %q, want %q", cfg.Storage.SharedDir, ".")
	}
	// All permissions default to true.
	if !cfg.Permissions.Download || !cfg.Permissions.Upload || !cfg.Permissions.Delete ||
		!cfg.Permissions.Rename || !cfg.Permissions.Mkdir {
		t.Error("All permission defaults should be true")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.ini")
	if err == nil {
		t.Fatal("LoadConfig should fail for missing file")
	}
}

func TestLoadConfig_AuthDisabled(t *testing.T) {
	path := writeTestConfig(t, `
[auth]
enabled = false
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Auth.Enabled {
		t.Error("Auth.Enabled should be false")
	}
}

func TestLoadConfig_UploadLimits(t *testing.T) {
	path := writeTestConfig(t, `
[permissions]
download = true
upload   = true
max_upload_file_size = 104857600
max_ip_files = 50
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Permissions.MaxUploadFileSize != 104857600 {
		t.Errorf("MaxUploadFileSize: got %d, want 104857600", cfg.Permissions.MaxUploadFileSize)
	}
	if cfg.Permissions.MaxIPFiles != 50 {
		t.Errorf("MaxIPFiles: got %d, want 50", cfg.Permissions.MaxIPFiles)
	}
}

func TestLoadConfig_UploadLimitsDefaults(t *testing.T) {
	path := writeTestConfig(t, ``)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Permissions.MaxUploadFileSize != 0 {
		t.Errorf("Default MaxUploadFileSize: got %d, want 0 (unlimited)", cfg.Permissions.MaxUploadFileSize)
	}
	if cfg.Permissions.MaxIPFiles != 0 {
		t.Errorf("Default MaxIPFiles: got %d, want 0 (unlimited)", cfg.Permissions.MaxIPFiles)
	}
}

func TestSaveConfig_UploadLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.ini")

	c := &Config{}
	c.Auth.Enabled = true
	c.Auth.Username = "anonymous"
	c.Permissions = Permissions{
		Download:          true,
		Upload:            true,
		Delete:            true,
		Rename:            true,
		Mkdir:             true,
		MaxUploadFileSize: 209715200,
		MaxIPFiles:        20,
	}
	c.Network.Port = 2121
	c.Network.MaxConnections = 10
	c.Storage.SharedDir = "."

	if err := SaveConfig(path, c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Reload and verify
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after save: %v", err)
	}
	if cfg.Permissions.MaxUploadFileSize != 209715200 {
		t.Errorf("MaxUploadFileSize roundtrip: got %d, want 209715200", cfg.Permissions.MaxUploadFileSize)
	}
	if cfg.Permissions.MaxIPFiles != 20 {
		t.Errorf("MaxIPFiles roundtrip: got %d, want 20", cfg.Permissions.MaxIPFiles)
	}
}

func TestLoadConfig_PartialPermissions(t *testing.T) {
	path := writeTestConfig(t, `
[permissions]
download = true
upload = false
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Permissions.Download {
		t.Error("Download should be true")
	}
	if cfg.Permissions.Upload {
		t.Error("Upload should be false")
	}
	// Unset fields keep defaults (true).
	if !cfg.Permissions.Delete {
		t.Error("Delete should default to true")
	}
}

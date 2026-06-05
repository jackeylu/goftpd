package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// TestResolveSharedDir_AbsolutePath verifies that resolveSharedDir converts
// relative paths to absolute paths.
func TestResolveSharedDir_AbsolutePath(t *testing.T) {
	cases := []struct {
		input string
		name  string
	}{
		{".", "dot"},
		{"", "empty"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := resolveSharedDir(tc.input)
			if !filepath.IsAbs(resolved) {
				t.Errorf("resolveSharedDir(%q) = %q, want absolute path", tc.input, resolved)
			}
		})
	}
}

// TestNewFTPDriver_ResolvesDotPath verifies that NewFTPDriver resolves
// shared_dir="." to an absolute path, fixing the BasePathFs bug.
func TestNewFTPDriver_ResolvesDotPath(t *testing.T) {
	cfg := &Config{
		Storage: struct{ SharedDir string }{SharedDir: "."},
		Permissions: Permissions{
			Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true,
		},
		Network: struct {
			Port           int
			MaxConnections int
		}{Port: 2121, MaxConnections: 10},
	}

	driver := NewFTPDriver(cfg)
	// The jailed fs should resolve "/" correctly now.
	_, err := driver.jailed.Stat("/")
	if err != nil {
		t.Fatalf("NewFTPDriver with shared_dir='.': Stat(/) failed: %v", err)
	}
}

// TestAbsoluteBasePathFs verifies that using an absolute path as BasePathFs
// root correctly resolves FTP-style paths.
func TestAbsoluteBasePathFs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	afero.WriteFile(afero.NewOsFs(), filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644)

	// Use absolute path — this should work correctly
	absFs := afero.NewBasePathFs(afero.NewOsFs(), tmpDir)

	_, err := absFs.Stat("/test.txt")
	if err != nil {
		t.Fatalf("Stat(/test.txt) with absolute base: %v", err)
	}

	err = absFs.Remove("/test.txt")
	if err != nil {
		t.Fatalf("Remove(/test.txt) with absolute base: %v", err)
	}

	// File should be gone
	_, err = absFs.Stat("/test.txt")
	if err == nil {
		t.Fatal("file should be deleted")
	}
}

// TestHotReload_Permissions verifies that UpdateConfig changes permissions
// for new FTP sessions without restarting the server.
func TestHotReload_Permissions(t *testing.T) {
	cfg := &Config{
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 2121, MaxConnections: 10},
		Storage:     struct{ SharedDir string }{SharedDir: t.TempDir()},
	}

	driver := NewFTPDriver(cfg)

	// Session 1: full permissions
	cd1, err := driver.AuthUser(nil, "user", "pass")
	if err != nil {
		t.Fatalf("AuthUser session 1: %v", err)
	}
	fs1 := cd1.(*PermissionFs)

	// Session 1 can create files
	f, err := fs1.Create("test.txt")
	if err != nil {
		t.Fatalf("session 1 Create should succeed: %v", err)
	}
	f.Close()

	// Hot-reload: disable upload
	newCfg := *cfg
	newCfg.Permissions = Permissions{Download: true, Upload: false, Delete: true, Rename: true, Mkdir: true}
	driver.UpdateConfig(&newCfg)

	// Session 2: upload disabled
	cd2, err := driver.AuthUser(nil, "user", "pass")
	if err != nil {
		t.Fatalf("AuthUser session 2: %v", err)
	}
	fs2 := cd2.(*PermissionFs)

	_, err = fs2.Create("test2.txt")
	if err != os.ErrPermission {
		t.Fatalf("session 2 Create should be denied, got: %v", err)
	}

	// Session 1 also sees updated permissions (shared pointer)
	_, err = fs1.Create("test3.txt")
	if err != os.ErrPermission {
		t.Fatalf("session 1 Create should be denied after reload, got: %v", err)
	}
}

// TestHotReload_Auth verifies that UpdateConfig changes auth credentials
// for new FTP sessions.
func TestHotReload_Auth(t *testing.T) {
	cfg := &Config{
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: true, Username: "admin", Password: "123"},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 2121, MaxConnections: 10},
		Storage:     struct{ SharedDir string }{SharedDir: t.TempDir()},
	}

	driver := NewFTPDriver(cfg)

	// Old credentials work
	_, err := driver.AuthUser(nil, "admin", "123")
	if err != nil {
		t.Fatalf("old credentials should work: %v", err)
	}

	// Wrong credentials fail
	_, err = driver.AuthUser(nil, "admin", "wrong")
	if err == nil {
		t.Fatal("wrong password should fail")
	}

	// Hot-reload: change username
	newCfg := *cfg
	newCfg.Auth = struct{ Enabled bool; Username string; Password string }{Enabled: true, Username: "root", Password: "456"}
	driver.UpdateConfig(&newCfg)

	// Old credentials now fail
	_, err = driver.AuthUser(nil, "admin", "123")
	if err == nil {
		t.Fatal("old credentials should fail after reload")
	}

	// New credentials work
	_, err = driver.AuthUser(nil, "root", "456")
	if err != nil {
		t.Fatalf("new credentials should work: %v", err)
	}
}

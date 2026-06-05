package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

// --- Helper function tests ---

func TestStrVal(t *testing.T) {
	if got := strVal("hello", "default"); got != "hello" {
		t.Errorf("strVal(string): got %q, want %q", got, "hello")
	}
	if got := strVal(42, "default"); got != "default" {
		t.Errorf("strVal(int): got %q, want %q", got, "default")
	}
	if got := strVal(nil, "default"); got != "default" {
		t.Errorf("strVal(nil): got %q, want %q", got, "default")
	}
}

func TestBoolVal(t *testing.T) {
	if got := boolVal(true, false); got != true {
		t.Errorf("boolVal(true): got %v, want true", got)
	}
	if got := boolVal(false, true); got != false {
		t.Errorf("boolVal(false): got %v, want false", got)
	}
	if got := boolVal("notbool", true); got != true {
		t.Errorf("boolVal(string): got %v, want default true", got)
	}
	if got := boolVal(nil, false); got != false {
		t.Errorf("boolVal(nil): got %v, want default false", got)
	}
}

func TestIntVal(t *testing.T) {
	cases := []struct {
		input interface{}
		def   int
		want  int
	}{
		{float64(42), 0, 42},
		{"99", 0, 99},
		{"notanumber", 7, 7},
		{nil, 10, 10},
	}
	for _, tc := range cases {
		got := intVal(tc.input, tc.def)
		if got != tc.want {
			t.Errorf("intVal(%v, %d) = %d, want %d", tc.input, tc.def, got, tc.want)
		}
	}
}

func TestInt64Val(t *testing.T) {
	cases := []struct {
		input interface{}
		def   int64
		want  int64
	}{
		{float64(104857600), 0, 104857600},
		{"209715200", 0, 209715200},
		{"notanumber", 99, 99},
		{nil, 50, 50},
	}
	for _, tc := range cases {
		got := int64Val(tc.input, tc.def)
		if got != tc.want {
			t.Errorf("int64Val(%v, %d) = %d, want %d", tc.input, tc.def, got, tc.want)
		}
	}
}

// --- configToMap / mapToConfig roundtrip ---

func TestConfigToMap_MapToConfig_Roundtrip(t *testing.T) {
	original := defaultConfig()
	original.Auth.Enabled = true
	original.Auth.Username = "admin"
	original.Auth.Password = "secret"
	original.Permissions.Download = true
	original.Permissions.Upload = true
	original.Permissions.Delete = false
	original.Permissions.Rename = true
	original.Permissions.Mkdir = false
	original.Permissions.MaxUploadFileSize = 104857600
	original.Permissions.MaxIPFiles = 20
	original.Network.Port = 2121
	original.Network.MaxConnections = 50
	original.Storage.SharedDir = "/tmp/ftp"

	// Forward: config → map
	m := configToMap(original)

	// Reverse: map → config
	restored := mapToConfig(m)

	// Verify all fields survived the roundtrip
	if restored.Auth.Enabled != original.Auth.Enabled {
		t.Errorf("Auth.Enabled: got %v, want %v", restored.Auth.Enabled, original.Auth.Enabled)
	}
	if restored.Auth.Username != original.Auth.Username {
		t.Errorf("Auth.Username: got %q, want %q", restored.Auth.Username, original.Auth.Username)
	}
	if restored.Auth.Password != original.Auth.Password {
		t.Errorf("Auth.Password: got %q, want %q", restored.Auth.Password, original.Auth.Password)
	}
	if restored.Permissions.Download != original.Permissions.Download {
		t.Errorf("Permissions.Download: got %v, want %v", restored.Permissions.Download, original.Permissions.Download)
	}
	if restored.Permissions.Upload != original.Permissions.Upload {
		t.Errorf("Permissions.Upload: got %v, want %v", restored.Permissions.Upload, original.Permissions.Upload)
	}
	if restored.Permissions.Delete != original.Permissions.Delete {
		t.Errorf("Permissions.Delete: got %v, want %v", restored.Permissions.Delete, original.Permissions.Delete)
	}
	if restored.Permissions.Rename != original.Permissions.Rename {
		t.Errorf("Permissions.Rename: got %v, want %v", restored.Permissions.Rename, original.Permissions.Rename)
	}
	if restored.Permissions.Mkdir != original.Permissions.Mkdir {
		t.Errorf("Permissions.Mkdir: got %v, want %v", restored.Permissions.Mkdir, original.Permissions.Mkdir)
	}
	if restored.Permissions.MaxUploadFileSize != original.Permissions.MaxUploadFileSize {
		t.Errorf("Permissions.MaxUploadFileSize: got %d, want %d", restored.Permissions.MaxUploadFileSize, original.Permissions.MaxUploadFileSize)
	}
	if restored.Permissions.MaxIPFiles != original.Permissions.MaxIPFiles {
		t.Errorf("Permissions.MaxIPFiles: got %d, want %d", restored.Permissions.MaxIPFiles, original.Permissions.MaxIPFiles)
	}
	if restored.Network.Port != original.Network.Port {
		t.Errorf("Network.Port: got %d, want %d", restored.Network.Port, original.Network.Port)
	}
	if restored.Network.MaxConnections != original.Network.MaxConnections {
		t.Errorf("Network.MaxConnections: got %d, want %d", restored.Network.MaxConnections, original.Network.MaxConnections)
	}
	if restored.Storage.SharedDir != original.Storage.SharedDir {
		t.Errorf("Storage.SharedDir: got %q, want %q", restored.Storage.SharedDir, original.Storage.SharedDir)
	}
}

func TestMapToConfig_InvalidTypes(t *testing.T) {
	// mapToConfig should handle missing/invalid sections gracefully
	m := map[string]interface{}{
		"auth": "not-a-map",
		"permissions": 42,
	}
	cfg := mapToConfig(m)
	// Should fall back to defaults
	if cfg.Auth.Username != "anonymous" {
		t.Errorf("Expected default username, got %q", cfg.Auth.Username)
	}
	if !cfg.Permissions.Download {
		t.Error("Expected default Download=true")
	}
}

func TestConfigToMap_AllKeysPresent(t *testing.T) {
	cfg := defaultConfig()
	m := configToMap(cfg)

	auth, ok := m["auth"].(map[string]interface{})
	if !ok {
		t.Fatal("configToMap missing 'auth' section")
	}
	for _, key := range []string{"enabled", "username", "password"} {
		if _, exists := auth[key]; !exists {
			t.Errorf("auth section missing key %q", key)
		}
	}

	perm, ok := m["permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("configToMap missing 'permissions' section")
	}
	for _, key := range []string{"download", "upload", "delete", "rename", "mkdir", "max_upload_file_size", "max_ip_files"} {
		if _, exists := perm[key]; !exists {
			t.Errorf("permissions section missing key %q", key)
		}
	}

	net, ok := m["network"].(map[string]interface{})
	if !ok {
		t.Fatal("configToMap missing 'network' section")
	}
	for _, key := range []string{"port", "max_connections"} {
		if _, exists := net[key]; !exists {
			t.Errorf("network section missing key %q", key)
		}
	}

	store, ok := m["storage"].(map[string]interface{})
	if !ok {
		t.Fatal("configToMap missing 'storage' section")
	}
	if _, exists := store["shared_dir"]; !exists {
		t.Error("storage section missing key 'shared_dir'")
	}
}

// --- HTTP handler tests ---

func TestHandleGetConfig_MissingFile(t *testing.T) {
	w := httptest.NewRecorder()
	handleGetConfig(w, "/nonexistent/path/config.ini")

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Response is not valid JSON: %v", err)
	}
	// Should return defaults when file doesn't exist
	auth, _ := result["auth"].(map[string]interface{})
	if auth["username"] != "anonymous" {
		t.Errorf("Expected default username 'anonymous', got %v", auth["username"])
	}
}

func TestHandleGetConfig_ValidFile(t *testing.T) {
	path := writeTestConfig(t, `
[auth]
enabled = true
username = admin
password = s3cret

[permissions]
upload = false
max_upload_file_size = 209715200
max_ip_files = 10

[network]
port = 2121

[storage]
shared_dir = /tmp/test
`)

	w := httptest.NewRecorder()
	handleGetConfig(w, path)

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)

	auth, _ := result["auth"].(map[string]interface{})
	if auth["username"] != "admin" {
		t.Errorf("Username: got %v, want admin", auth["username"])
	}

	perm, _ := result["permissions"].(map[string]interface{})
	if perm["upload"] != false {
		t.Errorf("Upload: got %v, want false", perm["upload"])
	}
	if perm["max_upload_file_size"] != float64(209715200) {
		t.Errorf("MaxUploadFileSize: got %v, want 209715200", perm["max_upload_file_size"])
	}
	if perm["max_ip_files"] != float64(10) {
		t.Errorf("MaxIPFiles: got %v, want 10", perm["max_ip_files"])
	}
}

func TestHandleSaveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.ini")

	body := `{
		"auth": {"enabled": true, "username": "testuser", "password": "pw123"},
		"permissions": {"download": true, "upload": true, "delete": true, "rename": true, "mkdir": true, "max_upload_file_size": 1048576, "max_ip_files": 5},
		"network": {"port": 2121, "max_connections": 10},
		"storage": {"shared_dir": "."}
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleSaveConfig(w, req, path)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify file was actually written
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after save: %v", err)
	}
	if cfg.Auth.Username != "testuser" {
		t.Errorf("Username: got %q, want %q", cfg.Auth.Username, "testuser")
	}
	if cfg.Permissions.MaxUploadFileSize != 1048576 {
		t.Errorf("MaxUploadFileSize: got %d, want 1048576", cfg.Permissions.MaxUploadFileSize)
	}
	if cfg.Permissions.MaxIPFiles != 5 {
		t.Errorf("MaxIPFiles: got %d, want 5", cfg.Permissions.MaxIPFiles)
	}
}

func TestHandleSaveConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.ini")

	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	handleSaveConfig(w, req, path)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestHandleGetLogs_Empty(t *testing.T) {
	// Reset global state
	globalLogBuf = nil

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	handleGetLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var result []LogEntry
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("Expected empty logs, got %d entries", len(result))
	}
}

func TestHandleGetLogs_WithData(t *testing.T) {
	buf := NewLogBuffer(10)
	buf.Write([]byte("test log line\n"))
	globalLogBuf = buf
	defer func() { globalLogBuf = nil }()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	handleGetLogs(w, req)

	var result []LogEntry
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(result))
	}
	if result[0].Msg != "test log line" {
		t.Errorf("Msg: got %q, want %q", result[0].Msg, "test log line")
	}
}

func TestHandleGetLogs_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	w := httptest.NewRecorder()
	handleGetLogs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405, got %d", w.Code)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"status": "ok"})

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("Body: got %v", result)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.Auth.Enabled {
		t.Error("Default Auth.Enabled should be true")
	}
	if cfg.Auth.Username != "anonymous" {
		t.Errorf("Default Auth.Username: got %q, want %q", cfg.Auth.Username, "anonymous")
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
	if !cfg.Permissions.Download || !cfg.Permissions.Upload {
		t.Error("Default permissions should be all true")
	}
	if cfg.Permissions.MaxUploadFileSize != 0 {
		t.Errorf("Default MaxUploadFileSize should be 0, got %d", cfg.Permissions.MaxUploadFileSize)
	}
	if cfg.Permissions.MaxIPFiles != 0 {
		t.Errorf("Default MaxIPFiles should be 0, got %d", cfg.Permissions.MaxIPFiles)
	}
}


func TestHandleStartServer_AlreadyRunning(t *testing.T) {
	// Reset global state
	ftpSrv = nil
	currentDriver = nil
	serverDone = nil

	// Simulate a running server by creating a real one with a test driver
	tmpDir := t.TempDir()
	testCfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: tmpDir},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 2121, MaxConnections: 10},
	}
	driver := NewFTPDriver(testCfg)
	ftpSrv = ftpserver.NewFtpServer(driver)

	req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handleStartServer(w, req)

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["error"] != "server already running" {
		t.Errorf("Expected 'server already running' error, got %v", result)
	}

	ftpSrv = nil
}

func TestHandleStopServer_NotRunning(t *testing.T) {
	ftpSrv = nil

	req := httptest.NewRequest(http.MethodPost, "/api/stop", nil)
	w := httptest.NewRecorder()
	handleStopServer(w, req)

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "already stopped" {
		t.Errorf("Expected 'already stopped', got %v", result)
	}
}

func TestHandleReloadServer_NotRunning(t *testing.T) {
	currentDriver = nil

	req := httptest.NewRequest(http.MethodPost, "/api/reload", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handleReloadServer(w, req)

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["error"] != "server not running" {
		t.Errorf("Expected 'server not running' error, got %v", result)
	}
}

func TestHandleStopServer_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stop", nil)
	w := httptest.NewRecorder()
	handleStopServer(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405, got %d", w.Code)
	}
}

func TestHandleStartServer_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/start", nil)
	w := httptest.NewRecorder()
	handleStartServer(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405, got %d", w.Code)
	}
}

func TestHandleReloadServer_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	w := httptest.NewRecorder()
	handleReloadServer(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405, got %d", w.Code)
	}
}

func TestHandleSaveConfig_MethodCheck(t *testing.T) {
	// handleSaveConfig is called directly, not via a switch — but let's verify
	// it works with a proper POST request body
	dir := t.TempDir()
	path := filepath.Join(dir, "save.ini")

	body := `{"auth":{"enabled":false},"permissions":{"download":true,"upload":true,"delete":true,"rename":true,"mkdir":true,"max_upload_file_size":0,"max_ip_files":0},"network":{"port":21,"max_connections":100},"storage":{"shared_dir":"."}}`
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleSaveConfig(w, req, path)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify roundtrip: saved config should be loadable
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Auth.Enabled {
		t.Error("Auth.Enabled should be false")
	}
}

func TestHandleSaveConfig_WritableDir(t *testing.T) {
	// Verify that SaveConfig can write to a new path
	dir := t.TempDir()
	path := filepath.Join(dir, "new.ini")

	// File doesn't exist yet — SaveConfig should create it
	body := `{"auth":{"enabled":true,"username":"test","password":"pw"},"permissions":{"download":true,"upload":true,"delete":true,"rename":true,"mkdir":true,"max_upload_file_size":0,"max_ip_files":0},"network":{"port":2121,"max_connections":10},"storage":{"shared_dir":"/tmp"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleSaveConfig(w, req, path)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Config file should exist: %v", err)
	}
}

// --- Full HTTP routing tests (no browser needed) ---

// newTestMux creates the same HTTP mux as runGUI but without the browser/server startup.
func newTestMux(cfgPath string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetConfig(w, cfgPath)
		case http.MethodPost:
			handleSaveConfig(w, r, cfgPath)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/start", handleStartServer)
	mux.HandleFunc("/api/stop", handleStopServer)
	mux.HandleFunc("/api/reload", handleReloadServer)
	mux.HandleFunc("/api/logs", handleGetLogs)

	return mux
}

func TestHTTPRouting_ConfigGetPost(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.ini")

	SaveConfig(cfgPath, &Config{
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: true, Username: "admin", Password: ""},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 2121, MaxConnections: 10},
		Storage:     struct{ SharedDir string }{SharedDir: "."},
	})

	mux := newTestMux(cfgPath)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET /api/config
	resp, err := srv.Client().Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("GET status: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// POST /api/config — save new config with upload limits
	body := `{"auth":{"enabled":false,"username":"newuser","password":"pw"},"permissions":{"download":true,"upload":false,"delete":true,"rename":true,"mkdir":true,"max_upload_file_size":5242880,"max_ip_files":3},"network":{"port":2121,"max_connections":10},"storage":{"shared_dir":"/tmp"}}`
	resp, err = srv.Client().Post(srv.URL+"/api/config", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/config: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("POST status: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify file updated on disk
	loaded, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Auth.Username != "newuser" {
		t.Errorf("Username after POST: got %q, want %q", loaded.Auth.Username, "newuser")
	}
	if loaded.Permissions.Upload {
		t.Error("Upload should be false after POST")
	}
	if loaded.Permissions.MaxUploadFileSize != 5242880 {
		t.Errorf("MaxUploadFileSize: got %d, want 5242880", loaded.Permissions.MaxUploadFileSize)
	}
	if loaded.Permissions.MaxIPFiles != 3 {
		t.Errorf("MaxIPFiles: got %d, want 3", loaded.Permissions.MaxIPFiles)
	}
}

func TestHTTPRouting_MethodNotAllowed(t *testing.T) {
	mux := newTestMux(filepath.Join(t.TempDir(), "config.ini"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodDelete, "/api/config"},
		{http.MethodGet, "/api/start"},
		{http.MethodGet, "/api/stop"},
		{http.MethodGet, "/api/reload"},
		{http.MethodPost, "/api/logs"},
	}
	for _, ep := range endpoints {
		req, _ := http.NewRequest(ep.method, srv.URL+ep.path, nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", ep.method, ep.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got %d, want 405", ep.method, ep.path, resp.StatusCode)
		}
	}
}

func TestHTTPRouting_LogsEndpoint(t *testing.T) {
	globalLogBuf = nil
	defer func() { globalLogBuf = nil }()

	mux := newTestMux(filepath.Join(t.TempDir(), "config.ini"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Before log capture — empty array
	resp, err := srv.Client().Get(srv.URL + "/api/logs")
	if err != nil {
		t.Fatalf("GET /api/logs: %v", err)
	}
	var logs []LogEntry
	json.NewDecoder(resp.Body).Decode(&logs)
	resp.Body.Close()
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs, got %d", len(logs))
	}

	// Set up buffer with data
	buf := NewLogBuffer(10)
	buf.Write([]byte("integration test log\n"))
	globalLogBuf = buf

	resp, err = srv.Client().Get(srv.URL + "/api/logs")
	if err != nil {
		t.Fatalf("GET /api/logs (with data): %v", err)
	}
	json.NewDecoder(resp.Body).Decode(&logs)
	resp.Body.Close()
	if len(logs) != 1 || logs[0].Msg != "integration test log" {
		t.Errorf("Expected 1 log entry, got: %v", logs)
	}
}

func TestHTTPRouting_ServerLifecycle(t *testing.T) {
	ftpSrv = nil
	currentDriver = nil
	serverDone = nil
	defer func() {
		ftpSrv = nil
		currentDriver = nil
		serverDone = nil
	}()

	mux := newTestMux(filepath.Join(t.TempDir(), "config.ini"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Stop when not running
	resp, err := srv.Client().Post(srv.URL+"/api/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/stop: %v", err)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if result["status"] != "already stopped" {
		t.Errorf("Stop when not running: got %v", result)
	}

	// Reload when not running
	resp, err = srv.Client().Post(srv.URL+"/api/reload", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /api/reload: %v", err)
	}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if result["error"] != "server not running" {
		t.Errorf("Reload when not running: got %v", result)
	}
}

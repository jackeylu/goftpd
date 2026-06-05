package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

//go:embed web/*
var webFS embed.FS

// globalLogBuf captures log output for the Web UI.
var globalLogBuf *LogBuffer

// runGUI starts an HTTP server and opens the browser.
func runGUI(cfgPath string) {
	// Capture logs for the web UI while keeping stderr output.
	globalLogBuf = NewLogBuffer(logBufSize)
	log.SetOutput(io.MultiWriter(os.Stderr, globalLogBuf))

	mux := http.NewServeMux()

	// Serve embedded static files.
	webContent, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(webContent)))

	// API endpoints.
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

	// Find available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to find available port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	url := fmt.Sprintf("http://127.0.0.1:%d", addr.Port)

	fmt.Printf("GoFTP Web UI: %s\n", url)

	// Open browser.
	go openBrowser(url)

	server := &http.Server{Handler: mux}
	if err := server.Serve(listener); err != nil {
		log.Fatalf("UI server error: %v", err)
	}
}

func handleGetConfig(w http.ResponseWriter, cfgPath string) {
	config, err := LoadConfig(cfgPath)
	if err != nil {
		config = defaultConfig()
	}
	writeJSON(w, configToMap(config))
}

func handleSaveConfig(w http.ResponseWriter, r *http.Request, cfgPath string) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := mapToConfig(req)
	if err := SaveConfig(cfgPath, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

var (
	ftpSrv        *ftpserver.FtpServer
	currentDriver *FTPDriver
	serverDone    chan struct{} // closed when FTP server goroutine exits
)

func handleStartServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ftpSrv != nil {
		writeJSON(w, map[string]string{"error": "server already running"})
		return
	}

	// Wait for previous server goroutine to fully exit.
	if serverDone != nil {
		<-serverDone
		serverDone = nil
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := mapToConfig(req)
	driver := NewFTPDriver(cfg)
	srv := ftpserver.NewFtpServer(driver)

	serverDone = make(chan struct{})
	go func() {
		defer close(serverDone)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("FTP server stopped: %v", err)
		}
	}()

	ftpSrv = srv
	currentDriver = driver

	writeJSON(w, map[string]interface{}{
		"status": "running",
		"port":   cfg.Network.Port,
	})
}

func handleStopServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ftpSrv == nil {
		writeJSON(w, map[string]string{"status": "already stopped"})
		return
	}
	ftpSrv.Stop()
	ftpSrv = nil
	currentDriver = nil
	log.Printf("[stop] FTP server stopped")
	writeJSON(w, map[string]string{"status": "stopped"})
}

func handleReloadServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if currentDriver == nil {
		writeJSON(w, map[string]string{"error": "server not running"})
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := mapToConfig(req)
	currentDriver.UpdateConfig(cfg)

	writeJSON(w, map[string]string{"status": "reloaded"})
}

func handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if globalLogBuf == nil {
		writeJSON(w, []LogEntry{})
		return
	}
	writeJSON(w, globalLogBuf.Recent(100))
}

// configToMap converts Config to a JSON-friendly map.
func configToMap(c *Config) map[string]interface{} {
	return map[string]interface{}{
		"auth": map[string]interface{}{
			"enabled":  c.Auth.Enabled,
			"username": c.Auth.Username,
			"password": c.Auth.Password,
		},
		"permissions": map[string]interface{}{
			"download": c.Permissions.Download,
			"upload":   c.Permissions.Upload,
			"delete":   c.Permissions.Delete,
			"rename":   c.Permissions.Rename,
			"mkdir":                c.Permissions.Mkdir,
			"max_upload_file_size": c.Permissions.MaxUploadFileSize,
			"max_ip_files":         c.Permissions.MaxIPFiles,
		},
		"network": map[string]interface{}{
			"port":            c.Network.Port,
			"max_connections": c.Network.MaxConnections,
		},
		"storage": map[string]interface{}{
			"shared_dir": c.Storage.SharedDir,
		},
	}
}

// mapToConfig builds a Config from a JSON-decoded map.
func mapToConfig(m map[string]interface{}) *Config {
	c := defaultConfig()

	if auth, ok := m["auth"].(map[string]interface{}); ok {
		c.Auth.Enabled = boolVal(auth["enabled"], true)
		c.Auth.Username = strVal(auth["username"], "anonymous")
		c.Auth.Password = strVal(auth["password"], "")
	}

	if perm, ok := m["permissions"].(map[string]interface{}); ok {
		c.Permissions.Download = boolVal(perm["download"], true)
		c.Permissions.Upload = boolVal(perm["upload"], true)
		c.Permissions.Delete = boolVal(perm["delete"], true)
		c.Permissions.Rename = boolVal(perm["rename"], true)
		c.Permissions.Mkdir = boolVal(perm["mkdir"], true)
		c.Permissions.MaxUploadFileSize = int64Val(perm["max_upload_file_size"], 0)
		c.Permissions.MaxIPFiles = intVal(perm["max_ip_files"], 0)
	}

	if net, ok := m["network"].(map[string]interface{}); ok {
		c.Network.Port = intVal(net["port"], 21)
		c.Network.MaxConnections = intVal(net["max_connections"], 100)
	}

	if store, ok := m["storage"].(map[string]interface{}); ok {
		c.Storage.SharedDir = strVal(store["shared_dir"], ".")
	}

	return c
}

func defaultConfig() *Config {
	c := &Config{}
	c.Auth.Enabled = true
	c.Auth.Username = "anonymous"
	c.Network.Port = 21
	c.Network.MaxConnections = 100
	c.Storage.SharedDir = "."
	c.Permissions = Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true}
	return c
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func strVal(v interface{}, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func boolVal(v interface{}, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func intVal(v interface{}, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		i, err := strconv.Atoi(n)
		if err == nil {
			return i
		}
	}
	return def
}

func int64Val(v interface{}, def int64) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err == nil {
			return i
		}
	}
	return def
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

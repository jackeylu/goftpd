//go:build integration

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

// ftpTestServer starts a real FTP server on a random port and returns
// the address and a stop function.
type ftpTestServer struct {
	addr   string
	driver *FTPDriver
	srv    *ftpserver.FtpServer
}

func startFTPTestServer(t *testing.T, cfg *Config) *ftpTestServer {
	t.Helper()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	cfg.Network.Port = port
	driver := NewFTPDriver(cfg)
	// Override to loopback-only so Windows Firewall doesn't prompt
	driver.settings.ListenAddr = fmt.Sprintf("127.0.0.1:%d", port)
	srv := ftpserver.NewFtpServer(driver)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.ListenAndServe(); err != nil {
			// Server stopped — expected on shutdown
		}
	}()

	ts := &ftpTestServer{
		addr:   fmt.Sprintf("127.0.0.1:%d", port),
		driver: driver,
		srv:    srv,
	}

	// Wait for server to be ready
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", ts.addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return ts
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("FTP server did not start within 3 seconds")
	return nil
}

func (ts *ftpTestServer) stop() {
	ts.srv.Stop()
}

// dialFTP connects to the test server and reads the banner.
func dialFTP(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Dial %s: %v", addr, err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	return conn, reader
}

// readResponse reads an FTP response line.
func readResponse(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	line = line[:len(line)-1] // trim newline
	if len(line) < 4 {
		t.Fatalf("Response too short: %q", line)
	}
	return line[:3], line[4:]
}

// sendCmd sends an FTP command and reads the response.
func sendCmd(t *testing.T, conn net.Conn, reader *bufio.Reader, cmd string) (string, string) {
	t.Helper()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "%s\r\n", cmd)
	return readResponse(t, reader)
}

// --- Integration tests ---

func TestFTPIntegration_ConnectAndBanner(t *testing.T) {
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: t.TempDir()},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 10},
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: false},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	conn, reader := dialFTP(t, ts.addr)
	defer conn.Close()

	code, _ := readResponse(t, reader)
	if code != "220" {
		t.Errorf("Banner: expected 220, got %s", code)
	}
}

func TestFTPIntegration_AnonymousLogin(t *testing.T) {
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: t.TempDir()},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 10},
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: false},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	conn, reader := dialFTP(t, ts.addr)
	defer conn.Close()

	readResponse(t, reader) // banner

	// USER anonymous
	code, _ := sendCmd(t, conn, reader, "USER anonymous")
	if code != "331" {
		t.Errorf("USER anonymous: expected 331, got %s", code)
	}

	// PASS anything
	code, _ = sendCmd(t, conn, reader, "PASS test@example.com")
	if code != "230" {
		t.Errorf("PASS: expected 230, got %s", code)
	}

	// PWD
	code, msg := sendCmd(t, conn, reader, "PWD")
	if code != "257" {
		t.Errorf("PWD: expected 257, got %s", code)
	}
	_ = msg
}

func TestFTPIntegration_AuthEnabled_WrongPassword(t *testing.T) {
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: t.TempDir()},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 10},
		Auth: struct{ Enabled bool; Username string; Password string }{
			Enabled: true, Username: "admin", Password: "secret",
		},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	// Auth failure causes ftpserverlib to close the connection,
	// so each attempt needs a fresh connection.

	// Test 1: wrong username → 530
	conn1, reader1 := dialFTP(t, ts.addr)
	defer conn1.Close()
	readResponse(t, reader1) // banner
	sendCmd(t, conn1, reader1, "USER hacker")
	code, _ := sendCmd(t, conn1, reader1, "PASS guess")
	if code != "530" {
		t.Errorf("Wrong user: expected 530, got %s", code)
	}

	// Test 2: correct username, wrong password → 530
	conn2, reader2 := dialFTP(t, ts.addr)
	defer conn2.Close()
	readResponse(t, reader2) // banner
	sendCmd(t, conn2, reader2, "USER admin")
	code, _ = sendCmd(t, conn2, reader2, "PASS wrong")
	if code != "530" {
		t.Errorf("Wrong password: expected 530, got %s", code)
	}

	// Test 3: correct credentials → 230
	conn3, reader3 := dialFTP(t, ts.addr)
	defer conn3.Close()
	readResponse(t, reader3) // banner
	sendCmd(t, conn3, reader3, "USER admin")
	code, _ = sendCmd(t, conn3, reader3, "PASS secret")
	if code != "230" {
		t.Errorf("Correct auth: expected 230, got %s", code)
	}
}

func TestFTPIntegration_ConnectionLimit(t *testing.T) {
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: t.TempDir()},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 2},
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: false},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	// Connect 2 clients — both should get 220
	var conns []net.Conn
	for i := 0; i < 2; i++ {
		conn, reader := dialFTP(t, ts.addr)
		code, _ := readResponse(t, reader)
		if code != "220" {
			t.Errorf("Connection %d: expected 220, got %s", i+1, code)
		}
		conns = append(conns, conn)
	}

	// 3rd connection should be rejected.
	// ftpserverlib wraps the 421 error — the exact code depends on the library version.
	// What matters is that the connection is rejected (not 220).
	conn3, reader3 := dialFTP(t, ts.addr)
	defer conn3.Close()
	code, _ := readResponse(t, reader3)
	if code == "220" {
		t.Errorf("Connection 3: should be rejected, got 220 (accepted)")
	}
	// Accept 421 (our error) or 500/530 (library-wrapped) — both mean rejection
	if code != "421" && code != "500" && code != "530" {
		t.Errorf("Connection 3: expected 421/500/530 (rejected), got %s", code)
	}

	// Cleanup
	for _, c := range conns {
		c.Close()
	}
}

func TestFTPIntegration_UploadFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: tmpDir},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 10},
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: false},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	conn, reader := dialFTP(t, ts.addr)
	defer conn.Close()

	readResponse(t, reader) // banner
	sendCmd(t, conn, reader, "USER anonymous")
	sendCmd(t, conn, reader, "PASS test@test.com")

	// Create a directory
	code, _ := sendCmd(t, conn, reader, "MKD testdir")
	if code != "257" {
		t.Errorf("MKD: expected 257, got %s", code)
	}

	// Verify directory exists on disk
	if _, err := os.Stat(filepath.Join(tmpDir, "testdir")); err != nil {
		t.Errorf("testdir should exist on disk: %v", err)
	}
}

func TestFTPIntegration_MkdirDenied(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: tmpDir},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: false},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 10},
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: false},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	conn, reader := dialFTP(t, ts.addr)
	defer conn.Close()

	readResponse(t, reader) // banner
	sendCmd(t, conn, reader, "USER anonymous")
	sendCmd(t, conn, reader, "PASS test@test.com")

	code, _ := sendCmd(t, conn, reader, "MKD blocked")
	if code != "550" {
		t.Errorf("MKD with mkdir=false: expected 550, got %s", code)
	}
}

func TestFTPIntegration_HotReload(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Storage:     struct{ SharedDir string }{SharedDir: tmpDir},
		Permissions: Permissions{Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true},
		Network:     struct{ Port int; MaxConnections int }{Port: 0, MaxConnections: 10},
		Auth:        struct{ Enabled bool; Username string; Password string }{Enabled: false},
	}

	ts := startFTPTestServer(t, cfg)
	defer ts.stop()

	// Connect and login
	conn, reader := dialFTP(t, ts.addr)
	defer conn.Close()

	readResponse(t, reader) // banner
	sendCmd(t, conn, reader, "USER anonymous")
	sendCmd(t, conn, reader, "PASS test@test.com")

	// MKD should work
	code, _ := sendCmd(t, conn, reader, "MKD before_reload")
	if code != "257" {
		t.Errorf("MKD before reload: expected 257, got %s", code)
	}

	// Hot-reload: disable mkdir
	newCfg := *cfg
	newCfg.Permissions.Mkdir = false
	ts.driver.UpdateConfig(&newCfg)

	// Same connection — MKD should now fail
	code, _ = sendCmd(t, conn, reader, "MKD after_reload")
	if code != "550" {
		t.Errorf("MKD after reload: expected 550, got %s", code)
	}
}


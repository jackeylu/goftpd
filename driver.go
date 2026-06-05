package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// FTPDriver implements ftpserver.MainDriver with hot-reloadable config.
type FTPDriver struct {
	mu       sync.RWMutex
	config   *Config
	perm     *Permissions    // shared pointer — updated in-place on reload, all PermissionFs see it
	tracker  *UploadTracker  // per-IP upload file count
	sem      chan struct{}
	jailed   afero.Fs
	settings *ftpserver.Settings
}

// NewFTPDriver creates a driver with connection limiting.
func NewFTPDriver(c *Config) *FTPDriver {
	base := afero.NewOsFs()
	absDir := resolveSharedDir(c.Storage.SharedDir)
	jailed := afero.NewBasePathFs(base, absDir)

	log.Printf("[init] Shared dir: %s (resolved: %s)", c.Storage.SharedDir, absDir)
	log.Printf("[init] Permissions: download=%v upload=%v delete=%v rename=%v mkdir=%v",
		c.Permissions.Download, c.Permissions.Upload,
		c.Permissions.Delete, c.Permissions.Rename, c.Permissions.Mkdir)
	log.Printf("[init] Auth: enabled=%v user=%s", c.Auth.Enabled, c.Auth.Username)
	log.Printf("[init] Passive ports: 21000-21099")

	return &FTPDriver{
		config:  c,
		perm:    &c.Permissions,
		tracker: NewUploadTracker(),
		sem:     make(chan struct{}, c.Network.MaxConnections),
		jailed:  jailed,
		settings: &ftpserver.Settings{
			ListenAddr:               fmt.Sprintf(":%d", c.Network.Port),
			PublicHost:               "127.0.0.1",
			PassiveTransferPortRange: ftpserver.PortRange{Start: 21000, End: 21099},
		},
	}
}

// UpdateConfig hot-reloads mutable config fields (auth, permissions).
// Permissions are updated in-place so existing sessions see the change immediately.
// Immutable fields (port, shared_dir, max_connections) are ignored.
func (d *FTPDriver) UpdateConfig(c *Config) {
	d.mu.Lock()
	d.config = c
	*d.perm = c.Permissions // update in-place — all PermissionFs instances see this
	d.mu.Unlock()
	log.Printf("[reload] Auth: enabled=%v user=%s", c.Auth.Enabled, c.Auth.Username)
	log.Printf("[reload] Permissions: download=%v upload=%v delete=%v rename=%v mkdir=%v",
		c.Permissions.Download, c.Permissions.Upload,
		c.Permissions.Delete, c.Permissions.Rename, c.Permissions.Mkdir)
}

// getConfig returns the current config (thread-safe).
func (d *FTPDriver) getConfig() *Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.config
}

// GetSettings returns cached FTP server settings.
func (d *FTPDriver) GetSettings() (*ftpserver.Settings, error) {
	return d.settings, nil
}

// ClientConnected checks the connection limit.
func (d *FTPDriver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	select {
	case d.sem <- struct{}{}:
		log.Printf("[connect] client #%d from %s", cc.ID(), cc.RemoteAddr())
		return "220 Welcome to GoFTP Server", nil
	default:
		log.Printf("[connect] rejected — max connections reached")
		return "", errors.New("421 Too many connections, try again later")
	}
}

// ClientDisconnected releases a connection slot.
func (d *FTPDriver) ClientDisconnected(cc ftpserver.ClientContext) {
	log.Printf("[disconnect] client #%d", cc.ID())
	<-d.sem
}

// AuthUser authenticates the client and returns a permission-gated filesystem.
// Reads current config via getConfig() so hot-reloaded changes take effect.
func (d *FTPDriver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	cfg := d.getConfig()
	addr := clientAddr(cc)

	if cfg.Auth.Enabled {
		if user != cfg.Auth.Username {
			log.Printf("[auth] denied user=%q from %s", user, addr)
			return nil, errors.New("530 Authentication failed")
		}
		// Anonymous users can use any password
		if user != "anonymous" && cfg.Auth.Password != "" && pass != cfg.Auth.Password {
			log.Printf("[auth] denied user=%q wrong password from %s", user, addr)
			return nil, errors.New("530 Authentication failed")
		}
	}
	log.Printf("[auth] ok user=%q %s", user, addr)
	return NewPermissionFsWithTracker(d.jailed, d.perm, d.tracker, ipFromAddr(addr)), nil
}

// clientAddr safely extracts the remote address from a ClientContext.
func clientAddr(cc ftpserver.ClientContext) string {
	if cc == nil {
		return "(test)"
	}
	return cc.RemoteAddr().String()
}

// ipFromAddr extracts the IP portion from an address string (strips port).
func ipFromAddr(addr string) string {
	// Handle "(test)" from nil ClientContext
	if addr == "(test)" {
		return "(test)"
	}
	// addr is typically "ip:port"
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// GetTLSConfig returns nil — TLS is not configured.
func (d *FTPDriver) GetTLSConfig() (*tls.Config, error) {
	return nil, nil
}

// resolveSharedDir converts a shared directory path to an absolute path.
// BasePathFs requires absolute paths to correctly resolve FTP-style paths.
func resolveSharedDir(dir string) string {
	if dir == "" || dir == "." {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

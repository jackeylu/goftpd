package main

import (
	"fmt"
	"log"
	"os"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// PermissionFs wraps an afero.Fs and enforces per-operation permissions.
// perm is a shared pointer: UpdateConfig modifies it in-place so all sessions
// see the latest permissions without re-authenticating.
type PermissionFs struct {
	afero.Fs
	perm     *Permissions
	tracker  *UploadTracker
	clientIP string
	name     string
}

// Permissions defines granular filesystem operation gates and upload limits.
type Permissions struct {
	Download          bool
	Upload            bool
	Delete            bool
	Rename            bool
	Mkdir             bool
	MaxUploadFileSize int64 // max bytes per uploaded file (0 = unlimited)
	MaxIPFiles        int   // max upload files per IP across sessions (0 = unlimited)
}

// NewPermissionFs creates a filesystem that enforces the given permissions.
// The caller must pass a pointer that stays alive; updates to *p are seen by all operations.
func NewPermissionFs(fs afero.Fs, p *Permissions) *PermissionFs {
	return NewPermissionFsWithTracker(fs, p, nil, "")
}

// NewPermissionFsWithTracker creates a filesystem with upload tracking per IP.
func NewPermissionFsWithTracker(fs afero.Fs, p *Permissions, tracker *UploadTracker, clientIP string) *PermissionFs {
	return &PermissionFs{
		Fs:       fs,
		perm:     p,
		tracker:  tracker,
		clientIP: clientIP,
		name:     fmt.Sprintf("PermissionFs(%s)", fs.Name()),
	}
}

// canUpload checks upload permission and per-IP file count limit.
func (p *PermissionFs) canUpload(name string) error {
	if !p.perm.Upload {
		log.Printf("[upload] DENIED %s (upload disabled)", name)
		return os.ErrPermission
	}
	if p.perm.MaxIPFiles > 0 && p.tracker != nil {
		if p.tracker.Count(p.clientIP) >= p.perm.MaxIPFiles {
			limit := p.perm.MaxIPFiles
			count := p.tracker.Count(p.clientIP)
			log.Printf("[upload] DENIED %s: IP %s has %d files, limit is %d", name, p.clientIP, count, limit)
			return fmt.Errorf("per-IP file limit reached (%d/%d files for %s): %w", count, limit, p.clientIP, ftpserver.ErrStorageExceeded)
		}
	}
	return nil
}

// wrapSizeLimit wraps a file with size enforcement if configured.
// Always wraps with tracker when available so per-IP counting works.
func (p *PermissionFs) wrapSizeLimit(f afero.File, name string) afero.File {
	hasTracker := p.tracker != nil && p.clientIP != ""

	// No limits and no tracking — return raw file
	if p.perm.MaxUploadFileSize == 0 && !hasTracker {
		return f
	}

	// Size limit uses shared pointer for hot-reload; no-limit passes &MaxUploadFileSize (== 0)
	parentFs := p.Fs
	if p.perm.MaxUploadFileSize == 0 {
		parentFs = nil // no cleanup needed when no size limit
	}
	slf := NewSizeLimitFile(f, &p.perm.MaxUploadFileSize, parentFs, name)
	if hasTracker {
		slf.WithTracker(p.tracker, p.clientIP)
	}
	return slf
}

// Create is gated by upload permission. Returns a SizeLimitFile if max_upload_file_size is set.
func (p *PermissionFs) Create(name string) (afero.File, error) {
	if err := p.canUpload(name); err != nil {
		return nil, err
	}
	f, err := p.Fs.Create(name)
	if err != nil {
		log.Printf("[upload] ERROR %s: %v", name, err)
		return nil, err
	}
	log.Printf("[upload] OK %s", name)
	return p.wrapSizeLimit(f, name), nil
}

// OpenFile routes to download or upload based on flags.
// Write-related flags require upload permission; read-only requires download permission.
func (p *PermissionFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	const writeFlags = os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_APPEND | os.O_TRUNC
	isWrite := flag&writeFlags != 0

	if isWrite {
		if err := p.canUpload(name); err != nil {
			return nil, err
		}
	} else {
		if !p.perm.Download {
			log.Printf("[download] DENIED %s (download disabled)", name)
			return nil, os.ErrPermission
		}
	}

	f, err := p.Fs.OpenFile(name, flag, perm)
	if err != nil {
		log.Printf("[%s] ERROR %s: %v", isWriteTag(isWrite), name, err)
		return nil, err
	}
	log.Printf("[%s] OK %s", isWriteTag(isWrite), name)

	if isWrite {
		return p.wrapSizeLimit(f, name), nil
	}
	return f, nil
}

func isWriteTag(isWrite bool) string {
	if isWrite {
		return "upload"
	}
	return "download"
}

// Open is gated by download permission.
func (p *PermissionFs) Open(name string) (afero.File, error) {
	if !p.perm.Download {
		log.Printf("[download] DENIED %s (download disabled)", name)
		return nil, os.ErrPermission
	}
	f, err := p.Fs.Open(name)
	if err != nil {
		log.Printf("[download] ERROR %s: %v", name, err)
	} else {
		log.Printf("[download] OK %s", name)
	}
	return f, err
}

// Remove is gated by delete permission. Decrements upload tracker.
func (p *PermissionFs) Remove(name string) error {
	if !p.perm.Delete {
		log.Printf("[delete] DENIED %s (delete disabled)", name)
		return os.ErrPermission
	}
	err := p.Fs.Remove(name)
	if err != nil {
		log.Printf("[delete] ERROR %s: %v", name, err)
	} else {
		log.Printf("[delete] OK %s", name)
		if p.tracker != nil && p.clientIP != "" {
			p.tracker.Decrement(p.clientIP)
		}
	}
	return err
}

// RemoveAll is gated by delete permission. Decrements upload tracker.
func (p *PermissionFs) RemoveAll(path string) error {
	if !p.perm.Delete {
		log.Printf("[delete] DENIED %s (delete disabled)", path)
		return os.ErrPermission
	}
	err := p.Fs.RemoveAll(path)
	if err != nil {
		log.Printf("[delete] ERROR %s: %v", path, err)
	} else {
		log.Printf("[delete] OK %s (recursive)", path)
		// Note: RemoveAll may delete multiple files but we only tracked individual
		// file uploads. We cannot know how many tracked files were under this path,
		// so we do not decrement here. Use Remove for precise per-file tracking.
	}
	return err
}

// Rename is gated by rename permission.
func (p *PermissionFs) Rename(oldname, newname string) error {
	if !p.perm.Rename {
		log.Printf("[rename] DENIED %s → %s (rename disabled)", oldname, newname)
		return os.ErrPermission
	}
	err := p.Fs.Rename(oldname, newname)
	if err != nil {
		log.Printf("[rename] ERROR %s → %s: %v", oldname, newname, err)
	} else {
		log.Printf("[rename] OK %s → %s", oldname, newname)
	}
	return err
}

// Mkdir is gated by mkdir permission.
func (p *PermissionFs) Mkdir(name string, perm os.FileMode) error {
	if !p.perm.Mkdir {
		log.Printf("[mkdir] DENIED %s (mkdir disabled)", name)
		return os.ErrPermission
	}
	err := p.Fs.Mkdir(name, perm)
	if err != nil {
		log.Printf("[mkdir] ERROR %s: %v", name, err)
	} else {
		log.Printf("[mkdir] OK %s", name)
	}
	return err
}

// MkdirAll is gated by mkdir permission.
func (p *PermissionFs) MkdirAll(path string, perm os.FileMode) error {
	if !p.perm.Mkdir {
		log.Printf("[mkdir] DENIED %s (mkdir disabled)", path)
		return os.ErrPermission
	}
	err := p.Fs.MkdirAll(path, perm)
	if err != nil {
		log.Printf("[mkdir] ERROR %s: %v", path, err)
	} else {
		log.Printf("[mkdir] OK %s (recursive)", path)
	}
	return err
}

// Chtimes is gated by upload permission (modifies file metadata).
func (p *PermissionFs) Chtimes(name string, atime, mtime time.Time) error {
	if !p.perm.Upload {
		return os.ErrPermission
	}
	return p.Fs.Chtimes(name, atime, mtime)
}

// Chmod is gated by upload permission (modifies file metadata).
func (p *PermissionFs) Chmod(name string, mode os.FileMode) error {
	if !p.perm.Upload {
		return os.ErrPermission
	}
	return p.Fs.Chmod(name, mode)
}

// Stat passes through and logs errors.
func (p *PermissionFs) Stat(name string) (os.FileInfo, error) {
	info, err := p.Fs.Stat(name)
	if err != nil {
		log.Printf("[fs] STAT %q error: %v", name, err)
	}
	return info, err
}

// Name returns a descriptive name for this filesystem.
func (p *PermissionFs) Name() string {
	return p.name
}

// ReadDir lists directory contents — used by ftpserverlib via
// the ClientDriverExtensionFileList interface.
func (p *PermissionFs) ReadDir(name string) ([]os.FileInfo, error) {
	entries, err := afero.ReadDir(p.Fs, name)
	if err != nil {
		log.Printf("[listdir] ERROR %s: %v", name, err)
	} else {
		log.Printf("[listdir] OK %s (%d entries)", name, len(entries))
	}
	return entries, err
}

// AllocateSpace implements ClientDriverExtensionAllocate.
// This is called when a client sends the ALLO command before uploading,
// allowing us to reject oversized files BEFORE the data transfer begins.
func (p *PermissionFs) AllocateSpace(size int) error {
	if p.perm.MaxUploadFileSize > 0 && int64(size) > p.perm.MaxUploadFileSize {
		log.Printf("[allo] DENIED: requested %s exceeds limit %s", humanBytes(int64(size)), humanBytes(p.perm.MaxUploadFileSize))
		return fmt.Errorf("requested %s exceeds max upload size %s: %w", humanBytes(int64(size)), humanBytes(p.perm.MaxUploadFileSize), ftpserver.ErrStorageExceeded)
	}
	return nil
}

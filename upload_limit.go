package main

import (
	"fmt"
	"log"
	"sync"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// ErrFileSizeExceeded is returned when a write exceeds the max upload file size.
// It wraps ftpserverlib's ErrStorageExceeded so the library maps it to
// FTP 552 (RFC 959: "exceeded storage allocation") instead of a generic 550.
var ErrFileSizeExceeded = fmt.Errorf("file size exceeds maximum allowed: %w", ftpserver.ErrStorageExceeded)

// humanBytes formats byte counts into a human-readable string.
func humanBytes(b int64) string {
	const (
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case b >= gb && b%gb == 0:
		return fmt.Sprintf("%dGB", b/gb)
	case b >= gb:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(gb))
	case b >= mb && b%mb == 0:
		return fmt.Sprintf("%dMB", b/mb)
	case b >= mb:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(mb))
	case b >= 1024 && b%1024 == 0:
		return fmt.Sprintf("%dKB", b/1024)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// UploadTracker tracks per-IP file upload counts across sessions.
type UploadTracker struct {
	mu     sync.Mutex
	counts map[string]int
}

// NewUploadTracker creates a new tracker.
func NewUploadTracker() *UploadTracker {
	return &UploadTracker{counts: make(map[string]int)}
}

// Count returns the current upload count for an IP.
func (t *UploadTracker) Count(ip string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counts[ip]
}

// Increment records one upload for the given IP.
func (t *UploadTracker) Increment(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counts[ip]++
}

// Decrement removes one upload count for the given IP (e.g. on file delete).
// Floors at zero.
func (t *UploadTracker) Decrement(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.counts[ip] > 0 {
		t.counts[ip]--
	}
}

// SizeLimitFile wraps an afero.File and enforces a max byte count on writes.
// When the limit is exceeded, Write returns ErrFileSizeExceeded.
// On Close, if the file was exceeded, the partial file is deleted from the parent fs.
type SizeLimitFile struct {
	afero.File
	maxSize  *int64 // shared pointer — hot-reloadable, 0 = unlimited
	written  int64
	exceeded bool
	parentFs afero.Fs   // for deleting partial files
	fileName string     // name for deletion
	tracker  *UploadTracker
	clientIP string
}

// NewSizeLimitFile creates a file that enforces a byte limit on writes.
// maxSize is a shared pointer for hot-reload. parentFs/fileName are used
// to clean up partial files when the limit is exceeded.
func NewSizeLimitFile(f afero.File, maxSize *int64, parentFs afero.Fs, fileName string) *SizeLimitFile {
	return &SizeLimitFile{
		File:     f,
		maxSize:  maxSize,
		parentFs: parentFs,
		fileName: fileName,
	}
}

// WithTracker attaches upload tracking to the file. Count increments on
// successful Close; decrements are handled by Remove/RemoveAll.
func (f *SizeLimitFile) WithTracker(t *UploadTracker, ip string) *SizeLimitFile {
	f.tracker = t
	f.clientIP = ip
	return f
}

// Write writes bytes to the underlying file, enforcing the size limit.
// When the limit would be exceeded, a partial write is performed (up to the limit)
// and ErrFileSizeExceeded is returned.
func (f *SizeLimitFile) Write(p []byte) (int, error) {
	limit := *f.maxSize
	if limit > 0 {
		remaining := limit - f.written
		if remaining <= 0 {
			f.exceeded = true
			log.Printf("[upload] DENIED %q: file size %s exceeds limit %s", f.fileName, humanBytes(f.written), humanBytes(limit))
			return 0, ErrFileSizeExceeded
		}
		if int64(len(p)) > remaining {
			// Write only up to the limit, then report error
			n, _ := f.File.Write(p[:remaining])
			f.written += int64(n)
			f.exceeded = true
			log.Printf("[upload] DENIED %q: file size %s exceeds limit %s", f.fileName, humanBytes(f.written), humanBytes(limit))
			return n, ErrFileSizeExceeded
		}
	}

	n, err := f.File.Write(p)
	f.written += int64(n)
	return n, err
}

// Close the file. If the file was exceeded, the partial file is deleted.
// On successful close, the upload tracker is incremented.
func (f *SizeLimitFile) Close() error {
	closeErr := f.File.Close()

	if f.exceeded && f.parentFs != nil && f.fileName != "" {
		// Clean up partial file
		if rmErr := f.parentFs.Remove(f.fileName); rmErr != nil {
			log.Printf("[upload] WARN failed to delete partial file %q: %v", f.fileName, rmErr)
		} else {
			log.Printf("[upload] deleted partial file %q (size limit exceeded)", f.fileName)
		}
	}

	// Track successful upload
	if !f.exceeded && f.tracker != nil && f.clientIP != "" {
		f.tracker.Increment(f.clientIP)
		log.Printf("[upload] tracked upload for IP %s", f.clientIP)
	}

	return closeErr
}

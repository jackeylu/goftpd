package main

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
)

// --- humanBytes tests ---

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{1, "1B"},
		{512, "512B"},
		{1024, "1KB"},
		{2048, "2KB"},
		{1024 * 1024, "1MB"},
		{5 * 1024 * 1024, "5MB"},
		{1024*1024 + 512*1024, "1.5MB"},
		{100 * 1024 * 1024, "100MB"},
		{1024 * 1024 * 1024, "1GB"},
		{2 * 1024 * 1024 * 1024, "2GB"},
		{int64(2.5 * float64(1024*1024*1024)), "2.5GB"},
		{104857600, "100MB"},   // real config value
		{209715200, "200MB"},   // real config value
		{1073741824, "1GB"},    // 1GiB
	}
	for _, tc := range cases {
		got := humanBytes(tc.input)
		if got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- SizeLimitFile tests ---

func TestSizeLimitFile_WriteWithinLimit(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("test.txt")

	maxSize := int64(100)
	slf := NewSizeLimitFile(f, &maxSize, nil, "")

	data := make([]byte, 50)
	n, err := slf.Write(data)
	if err != nil {
		t.Fatalf("Write within limit should succeed: %v", err)
	}
	if n != 50 {
		t.Errorf("Write: got %d bytes, want 50", n)
	}
	slf.Close()
}

func TestSizeLimitFile_WriteExactLimit(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("test.txt")

	maxSize := int64(100)
	slf := NewSizeLimitFile(f, &maxSize, nil, "")

	data := make([]byte, 100)
	n, err := slf.Write(data)
	if err != nil {
		t.Fatalf("Write at exact limit should succeed: %v", err)
	}
	if n != 100 {
		t.Errorf("Write: got %d bytes, want 100", n)
	}
	slf.Close()
}

func TestSizeLimitFile_WriteExceedsLimit(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("test.txt")

	maxSize := int64(100)
	slf := NewSizeLimitFile(f, &maxSize, nil, "")

	// First write: 80 bytes, OK
	data := make([]byte, 80)
	n, err := slf.Write(data)
	if err != nil {
		t.Fatalf("First write should succeed: %v", err)
	}
	if n != 80 {
		t.Errorf("First write: got %d, want 80", n)
	}

	// Second write: 30 bytes, exceeds 100 limit
	data2 := make([]byte, 30)
	_, err = slf.Write(data2)
	if err == nil {
		t.Fatal("Write exceeding limit should return error")
	}
	if !errors.Is(err, ErrFileSizeExceeded) {
		t.Errorf("Error type: got %v, want ErrFileSizeExceeded", err)
	}

	slf.Close()
}

func TestSizeLimitFile_WriteSingleOversizedChunk(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("test.txt")

	maxSize := int64(50)
	slf := NewSizeLimitFile(f, &maxSize, nil, "")

	data := make([]byte, 100)
	n, err := slf.Write(data)
	if err == nil {
		t.Fatal("Write 100 bytes with limit 50 should fail")
	}
	// Should write up to limit then error
	if n > 50 {
		t.Errorf("Should not write more than limit: wrote %d", n)
	}
	_ = n
	slf.Close()
}

func TestSizeLimitFile_ZeroLimitNoRestriction(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("test.txt")

	maxSize := int64(0) // unlimited
	slf := NewSizeLimitFile(f, &maxSize, nil, "")

	data := make([]byte, 10000)
	n, err := slf.Write(data)
	if err != nil {
		t.Fatalf("Write with limit=0 (unlimited) should succeed: %v", err)
	}
	if n != 10000 {
		t.Errorf("Write: got %d, want 10000", n)
	}
	slf.Close()
}

func TestSizeLimitFile_Close_DeletesPartialOnExceeded(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("test.txt")

	maxSize := int64(10)
	slf := NewSizeLimitFile(f, &maxSize, memfs, "partial.txt")

	// Exceed the limit
	slf.Write(make([]byte, 50))

	// Close should delete the file because it was exceeded
	slf.Close()

	_, err := memfs.Stat("partial.txt")
	if err == nil {
		t.Error("Partial file should be deleted after size exceeded")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected NotExist error, got: %v", err)
	}
}

func TestSizeLimitFile_Close_NoDeleteOnSuccess(t *testing.T) {
	memfs := afero.NewMemMapFs()
	f, _ := memfs.Create("good.txt")

	maxSize := int64(100)
	slf := NewSizeLimitFile(f, &maxSize, memfs, "good.txt")

	slf.Write(make([]byte, 50))
	slf.Close()

	_, err := memfs.Stat("good.txt")
	if err != nil {
		t.Errorf("Successful file should NOT be deleted: %v", err)
	}
}

// --- UploadTracker tests ---

func TestUploadTracker_IncrementDecrement(t *testing.T) {
	tracker := NewUploadTracker()

	tracker.Increment("1.2.3.4")
	tracker.Increment("1.2.3.4")
	tracker.Increment("5.6.7.8")

	if got := tracker.Count("1.2.3.4"); got != 2 {
		t.Errorf("Count(1.2.3.4) = %d, want 2", got)
	}
	if got := tracker.Count("5.6.7.8"); got != 1 {
		t.Errorf("Count(5.6.7.8) = %d, want 1", got)
	}

	tracker.Decrement("1.2.3.4")
	if got := tracker.Count("1.2.3.4"); got != 1 {
		t.Errorf("After decrement Count(1.2.3.4) = %d, want 1", got)
	}
}

func TestUploadTracker_CountNonExistent(t *testing.T) {
	tracker := NewUploadTracker()
	if got := tracker.Count("unknown"); got != 0 {
		t.Errorf("Count of unknown IP should be 0, got %d", got)
	}
}

func TestUploadTracker_DecrementFloorAtZero(t *testing.T) {
	tracker := NewUploadTracker()
	tracker.Decrement("1.2.3.4") // decrement below zero should not go negative
	if got := tracker.Count("1.2.3.4"); got != 0 {
		t.Errorf("Count should floor at 0, got %d", got)
	}
}

// --- PermissionFs integration: per-IP file count ---

func TestPermissionFs_Create_PerIPFileLimitReached(t *testing.T) {
	perm := Permissions{Upload: true, MaxIPFiles: 2}
	tracker := NewUploadTracker()
	tracker.Increment("1.2.3.4")
	tracker.Increment("1.2.3.4")

	fs := NewPermissionFsWithTracker(afero.NewMemMapFs(), &perm, tracker, "1.2.3.4")

	_, err := fs.Create("another.txt")
	if err == nil {
		t.Fatal("Create should be denied when per-IP file limit reached")
	}
}

func TestPermissionFs_Create_PerIPFileLimitNotReached(t *testing.T) {
	perm := Permissions{Upload: true, MaxIPFiles: 5}
	tracker := NewUploadTracker()
	tracker.Increment("1.2.3.4")
	tracker.Increment("1.2.3.4")

	fs := NewPermissionFsWithTracker(afero.NewMemMapFs(), &perm, tracker, "1.2.3.4")

	f, err := fs.Create("another.txt")
	if err != nil {
		t.Fatalf("Create should succeed when under limit: %v", err)
	}
	f.Close()
}

func TestPermissionFs_Create_PerIPFileLimitZero_NoLimit(t *testing.T) {
	perm := Permissions{Upload: true, MaxIPFiles: 0} // unlimited
	tracker := NewUploadTracker()
	// Add many files
	for i := 0; i < 100; i++ {
		tracker.Increment("1.2.3.4")
	}

	fs := NewPermissionFsWithTracker(afero.NewMemMapFs(), &perm, tracker, "1.2.3.4")

	f, err := fs.Create("yet-another.txt")
	if err != nil {
		t.Fatalf("Create should succeed when MaxIPFiles=0 (unlimited): %v", err)
	}
	f.Close()
}

func TestPermissionFs_Remove_TrackerDecrements(t *testing.T) {
	perm := Permissions{Upload: true, Delete: true, MaxIPFiles: 5}
	tracker := NewUploadTracker()
	tracker.Increment("1.2.3.4")

	memfs := afero.NewMemMapFs()
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	fs := NewPermissionFsWithTracker(memfs, &perm, tracker, "1.2.3.4")

	err := fs.Remove("file.txt")
	if err != nil {
		t.Fatalf("Remove should succeed: %v", err)
	}

	if got := tracker.Count("1.2.3.4"); got != 0 {
		t.Errorf("Tracker should be 0 after remove, got %d", got)
	}
}

func TestPermissionFs_RemoveAll_TrackerNoDecrement(t *testing.T) {
	perm := Permissions{Delete: true, MaxIPFiles: 5}
	tracker := NewUploadTracker()
	tracker.Increment("1.2.3.4")
	tracker.Increment("1.2.3.4")

	memfs := afero.NewMemMapFs()
	memfs.Mkdir("somedir", 0755)
	afero.WriteFile(memfs, "somedir/a.txt", []byte("a"), 0644)
	afero.WriteFile(memfs, "somedir/b.txt", []byte("b"), 0644)

	fs := NewPermissionFsWithTracker(memfs, &perm, tracker, "1.2.3.4")

	err := fs.RemoveAll("somedir")
	if err != nil {
		t.Fatalf("RemoveAll should succeed: %v", err)
	}

	// RemoveAll does not decrement — it can't know how many tracked files were inside.
	// Only Remove decrements for precise per-file tracking.
	if got := tracker.Count("1.2.3.4"); got != 2 {
		t.Errorf("Tracker should remain 2 after RemoveAll (cannot count children), got %d", got)
	}
}

// --- PermissionFs: size-limited Create/OpenFile ---

func TestPermissionFs_Create_ReturnsSizeLimitFile(t *testing.T) {
	maxSize := int64(100)
	perm := Permissions{Upload: true, MaxUploadFileSize: maxSize}
	tracker := NewUploadTracker()

	memfs := afero.NewMemMapFs()
	fs := NewPermissionFsWithTracker(memfs, &perm, tracker, "1.2.3.4")

	f, err := fs.Create("bigfile.txt")
	if err != nil {
		t.Fatalf("Create should succeed: %v", err)
	}

	// Write within limit
	n, err := f.Write(make([]byte, 50))
	if err != nil {
		t.Fatalf("Write 50 bytes should succeed: %v", err)
	}
	if n != 50 {
		t.Errorf("Wrote %d, want 50", n)
	}

	// Write exceeding limit
	_, err = f.Write(make([]byte, 60))
	if err == nil {
		t.Fatal("Write exceeding 100 limit should fail")
	}

	f.Close()
}

func TestPermissionFs_Create_NoSizeLimit(t *testing.T) {
	perm := Permissions{Upload: true, MaxUploadFileSize: 0} // unlimited
	tracker := NewUploadTracker()

	memfs := afero.NewMemMapFs()
	fs := NewPermissionFsWithTracker(memfs, &perm, tracker, "1.2.3.4")

	f, err := fs.Create("bigfile.txt")
	if err != nil {
		t.Fatalf("Create should succeed: %v", err)
	}

	// Write a lot — should be fine
	n, err := f.Write(make([]byte, 100000))
	if err != nil {
		t.Fatalf("Write with no limit should succeed: %v", err)
	}
	if n != 100000 {
		t.Errorf("Wrote %d, want 100000", n)
	}
	f.Close()
}

func TestPermissionFs_OpenFile_WriteFlags_SizeLimit(t *testing.T) {
	maxSize := int64(50)
	perm := Permissions{Upload: true, MaxUploadFileSize: maxSize}
	tracker := NewUploadTracker()

	memfs := afero.NewMemMapFs()
	fs := NewPermissionFsWithTracker(memfs, &perm, tracker, "1.2.3.4")

	f, err := fs.OpenFile("file.txt", os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("OpenFile should succeed: %v", err)
	}

	_, err = f.Write(make([]byte, 100))
	if err == nil {
		t.Fatal("Write exceeding limit should fail")
	}
	f.Close()
}

// --- ALLO support (ClientDriverExtensionAllocate) ---

func TestPermissionFs_AllocateSpace_WithinLimit(t *testing.T) {
	perm := Permissions{Upload: true, MaxUploadFileSize: 1000}
	tracker := NewUploadTracker()
	fs := NewPermissionFsWithTracker(afero.NewMemMapFs(), &perm, tracker, "1.2.3.4")

	err := fs.AllocateSpace(500)
	if err != nil {
		t.Fatalf("AllocateSpace(500) with limit 1000 should succeed: %v", err)
	}
}

func TestPermissionFs_AllocateSpace_ExceedsLimit(t *testing.T) {
	perm := Permissions{Upload: true, MaxUploadFileSize: 1000}
	tracker := NewUploadTracker()
	fs := NewPermissionFsWithTracker(afero.NewMemMapFs(), &perm, tracker, "1.2.3.4")

	err := fs.AllocateSpace(2000)
	if err == nil {
		t.Fatal("AllocateSpace(2000) with limit 1000 should fail")
	}
}

func TestPermissionFs_AllocateSpace_ZeroLimit_NoLimit(t *testing.T) {
	perm := Permissions{Upload: true, MaxUploadFileSize: 0}
	tracker := NewUploadTracker()
	fs := NewPermissionFsWithTracker(afero.NewMemMapFs(), &perm, tracker, "1.2.3.4")

	err := fs.AllocateSpace(999999999)
	if err != nil {
		t.Fatalf("AllocateSpace with limit=0 (unlimited) should succeed: %v", err)
	}
}

// --- Hot-reload: limits update live ---

func TestHotReload_MaxUploadSize(t *testing.T) {
	cfg := &Config{
		Permissions: Permissions{
			Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true,
			MaxUploadFileSize: 1000,
		},
		Network: struct {
			Port           int
			MaxConnections int
		}{Port: 2121, MaxConnections: 10},
		Storage: struct{ SharedDir string }{SharedDir: t.TempDir()},
	}

	driver := NewFTPDriver(cfg)
	cd, _ := driver.AuthUser(nil, "user", "pass")
	fs := cd.(*PermissionFs)

	// Before reload: limit is 1000
	err := fs.AllocateSpace(500)
	if err != nil {
		t.Fatalf("AllocateSpace(500) before reload: %v", err)
	}
	err = fs.AllocateSpace(2000)
	if err == nil {
		t.Fatal("AllocateSpace(2000) before reload should fail")
	}

	// Hot-reload: change limit to 5000
	newCfg := *cfg
	newCfg.Permissions.MaxUploadFileSize = 5000
	driver.UpdateConfig(&newCfg)

	err = fs.AllocateSpace(2000)
	if err != nil {
		t.Fatalf("AllocateSpace(2000) after reload should succeed: %v", err)
	}
}

func TestHotReload_MaxIPFiles(t *testing.T) {
	cfg := &Config{
		Permissions: Permissions{
			Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true,
			MaxIPFiles: 1,
		},
		Network: struct {
			Port           int
			MaxConnections int
		}{Port: 2121, MaxConnections: 10},
		Storage: struct{ SharedDir string }{SharedDir: t.TempDir()},
	}

	driver := NewFTPDriver(cfg)
	cd, _ := driver.AuthUser(nil, "user", "pass")
	fs := cd.(*PermissionFs)

	// Upload one file to fill the limit
	f, err := fs.Create("file1.txt")
	if err != nil {
		t.Fatalf("First Create should succeed: %v", err)
	}
	f.Close()

	// Second file should be denied
	_, err = fs.Create("file2.txt")
	if err == nil {
		t.Fatal("Second Create should be denied (limit=1)")
	}

	// Hot-reload: increase limit to 5
	newCfg := *cfg
	newCfg.Permissions.MaxIPFiles = 5
	driver.UpdateConfig(&newCfg)

	f, err = fs.Create("file2.txt")
	if err != nil {
		t.Fatalf("Third Create after reload should succeed: %v", err)
	}
	f.Close()
}

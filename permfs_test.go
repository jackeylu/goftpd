package main

import (
	"os"
	"testing"

	"github.com/spf13/afero"
)

// newTestFs creates an in-memory PermissionFs for testing.
func newTestFs(p Permissions) *PermissionFs {
	return NewPermissionFs(afero.NewMemMapFs(), &p)
}

// --- Download ---

func TestOpen_DownloadAllowed(t *testing.T) {
	fs := newTestFs(Permissions{Download: true})
	// Create a file via the underlying memfs directly.
	memfs := fs.Fs
	afero.WriteFile(memfs, "test.txt", []byte("hello"), 0644)

	f, err := fs.Open("test.txt")
	if err != nil {
		t.Fatalf("Open should succeed with download=true: %v", err)
	}
	f.Close()
}

func TestOpen_DownloadDenied(t *testing.T) {
	fs := newTestFs(Permissions{Download: false})
	memfs := fs.Fs
	afero.WriteFile(memfs, "test.txt", []byte("hello"), 0644)

	_, err := fs.Open("test.txt")
	if err != os.ErrPermission {
		t.Fatalf("Open should fail with ErrPermission when download=false, got: %v", err)
	}
}

// --- Upload ---

func TestCreate_UploadAllowed(t *testing.T) {
	fs := newTestFs(Permissions{Upload: true})
	f, err := fs.Create("newfile.txt")
	if err != nil {
		t.Fatalf("Create should succeed with upload=true: %v", err)
	}
	f.Close()
}

func TestCreate_UploadDenied(t *testing.T) {
	fs := newTestFs(Permissions{Upload: false})
	_, err := fs.Create("newfile.txt")
	if err != os.ErrPermission {
		t.Fatalf("Create should fail with ErrPermission when upload=false, got: %v", err)
	}
}

func TestOpenFile_WriteFlags_UploadDenied(t *testing.T) {
	fs := newTestFs(Permissions{Upload: false})
	flags := []int{
		os.O_WRONLY, os.O_RDWR, os.O_CREATE,
		os.O_APPEND, os.O_TRUNC,
	}
	for _, flag := range flags {
		_, err := fs.OpenFile("file.txt", flag, 0644)
		if err != os.ErrPermission {
			t.Errorf("OpenFile with flag %d should fail when upload=false, got: %v", flag, err)
		}
	}
}

func TestOpenFile_ReadFlag_UploadIrrelevant(t *testing.T) {
	fs := newTestFs(Permissions{Upload: false, Download: true})
	memfs := fs.Fs
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	f, err := fs.OpenFile("file.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile with O_RDONLY should succeed regardless of upload: %v", err)
	}
	f.Close()
}

func TestChmod_UploadDenied(t *testing.T) {
	fs := newTestFs(Permissions{Upload: false})
	memfs := fs.Fs
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	err := fs.Chmod("file.txt", 0777)
	if err != os.ErrPermission {
		t.Fatalf("Chmod should fail when upload=false, got: %v", err)
	}
}

// --- Delete ---

func TestRemove_DeleteAllowed(t *testing.T) {
	fs := newTestFs(Permissions{Delete: true})
	memfs := fs.Fs
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	err := fs.Remove("file.txt")
	if err != nil {
		t.Fatalf("Remove should succeed with delete=true: %v", err)
	}
}

func TestRemove_DeleteDenied(t *testing.T) {
	fs := newTestFs(Permissions{Delete: false})
	memfs := fs.Fs
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	err := fs.Remove("file.txt")
	if err != os.ErrPermission {
		t.Fatalf("Remove should fail with ErrPermission when delete=false, got: %v", err)
	}
}

func TestRemoveAll_DeleteDenied(t *testing.T) {
	fs := newTestFs(Permissions{Delete: false})
	err := fs.RemoveAll("somedir")
	if err != os.ErrPermission {
		t.Fatalf("RemoveAll should fail with ErrPermission when delete=false, got: %v", err)
	}
}

// --- Rename ---

func TestRename_RenameAllowed(t *testing.T) {
	fs := newTestFs(Permissions{Rename: true})
	memfs := fs.Fs
	afero.WriteFile(memfs, "old.txt", []byte("data"), 0644)

	err := fs.Rename("old.txt", "new.txt")
	if err != nil {
		t.Fatalf("Rename should succeed with rename=true: %v", err)
	}
}

func TestRename_RenameDenied(t *testing.T) {
	fs := newTestFs(Permissions{Rename: false})
	memfs := fs.Fs
	afero.WriteFile(memfs, "old.txt", []byte("data"), 0644)

	err := fs.Rename("old.txt", "new.txt")
	if err != os.ErrPermission {
		t.Fatalf("Rename should fail with ErrPermission when rename=false, got: %v", err)
	}
}

// --- Mkdir ---

func TestMkdir_MkdirAllowed(t *testing.T) {
	fs := newTestFs(Permissions{Mkdir: true})
	err := fs.Mkdir("testdir", 0755)
	if err != nil {
		t.Fatalf("Mkdir should succeed with mkdir=true: %v", err)
	}
}

func TestMkdir_MkdirDenied(t *testing.T) {
	fs := newTestFs(Permissions{Mkdir: false})
	err := fs.Mkdir("testdir", 0755)
	if err != os.ErrPermission {
		t.Fatalf("Mkdir should fail with ErrPermission when mkdir=false, got: %v", err)
	}
}

func TestMkdirAll_MkdirDenied(t *testing.T) {
	fs := newTestFs(Permissions{Mkdir: false})
	err := fs.MkdirAll("a/b/c", 0755)
	if err != os.ErrPermission {
		t.Fatalf("MkdirAll should fail with ErrPermission when mkdir=false, got: %v", err)
	}
}

// --- Name ---

func TestName(t *testing.T) {
	fs := newTestFs(Permissions{})
	name := fs.Name()
	if name == "" {
		t.Fatal("Name should return a non-empty string")
	}
}

// --- Stat (passthrough, should always work) ---

func TestStat_Passthrough(t *testing.T) {
	fs := newTestFs(Permissions{}) // all false
	memfs := fs.Fs
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	info, err := fs.Stat("file.txt")
	if err != nil {
		t.Fatalf("Stat should always pass through: %v", err)
	}
	if info.Size() != 4 {
		t.Fatalf("Stat size mismatch: got %d, want 4", info.Size())
	}
}

// --- All permissions denied (locked down) ---

func TestAllDenied_LockedDown(t *testing.T) {
	fs := newTestFs(Permissions{}) // all false
	memfs := fs.Fs
	afero.WriteFile(memfs, "file.txt", []byte("data"), 0644)

	if _, err := fs.Open("file.txt"); err == nil {
		t.Error("Open should be denied")
	}
	if _, err := fs.Create("new.txt"); err == nil {
		t.Error("Create should be denied")
	}
	if err := fs.Remove("file.txt"); err == nil {
		t.Error("Remove should be denied")
	}
	if err := fs.Rename("file.txt", "x"); err == nil {
		t.Error("Rename should be denied")
	}
	if err := fs.Mkdir("dir", 0755); err == nil {
		t.Error("Mkdir should be denied")
	}
	if err := fs.Chmod("file.txt", 0777); err == nil {
		t.Error("Chmod should be denied")
	}
}

// --- All permissions allowed (full access) ---

func TestAllAllowed_FullAccess(t *testing.T) {
	fs := newTestFs(Permissions{
		Download: true, Upload: true, Delete: true, Rename: true, Mkdir: true,
	})

	f, err := fs.Create("file.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.WriteString("hello")
	f.Close()

	f, err = fs.Open("file.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	f.Close()

	if err := fs.Mkdir("dir", 0755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := fs.Rename("file.txt", "renamed.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := fs.Remove("renamed.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := fs.Chmod("dir", 0777); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
}

package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAcquireInstanceLockRejectsSecondHolderUntilReleased(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	release, err := AcquireInstanceLock(configPath, 0)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	started := time.Now()
	if _, err := AcquireInstanceLock(configPath, 600*time.Millisecond); !errors.Is(err, ErrInstanceRunning) {
		t.Fatalf("second lock error = %v, want ErrInstanceRunning", err)
	}
	if waited := time.Since(started); waited < 500*time.Millisecond {
		t.Fatalf("second lock returned after %s, expected to wait", waited)
	}

	acquired := make(chan error, 1)
	go func() {
		releaseSecond, err := AcquireInstanceLock(configPath, 5*time.Second)
		if err == nil {
			releaseSecond()
		}
		acquired <- err
	}()
	time.Sleep(100 * time.Millisecond)
	release()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("waiting lock after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting lock was not acquired after release")
	}
}

func TestWriteFileWithLockReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.Repeat("old\n", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileWithLock(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("writeFileWithLock: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new\n" {
		t.Fatalf("content = %q, err = %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("existing file mode not preserved: %v", info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

package onion

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestPublishMatchRetriesWindowsReader(t *testing.T) {
	directory := t.TempDir()
	staging := filepath.Join(directory, "staging")
	destination := filepath.Join(directory, "published")

	err := os.Mkdir(staging, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	key := testKey()

	err = writeKeyFiles(staging, &key, key.Hostname())
	if err != nil {
		t.Fatal(err)
	}

	closeReader := lockPublishDirectory(t, staging)

	err = os.Rename(staging, destination)
	if !retryablePublishError(err) {
		t.Fatalf("reader did not block rename: %v", err)
	}

	time.AfterFunc(50*time.Millisecond, closeReader)

	err = publishMatch(staging, destination)
	if err != nil {
		t.Fatal(err)
	}

	hostname, err := os.ReadFile(filepath.Join(destination, "hostname"))
	if err != nil || string(hostname) != key.Hostname()+"\n" {
		t.Fatalf("published hostname: %q, %v", hostname, err)
	}

	_, err = os.Stat(staging)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging directory remains: %v", err)
	}
}

func TestPublishMatchRetainsLockedStaging(t *testing.T) {
	directory := t.TempDir()
	staging := filepath.Join(directory, "staging")
	destination := filepath.Join(directory, "published")

	err := os.Mkdir(staging, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	key := testKey()

	err = writeKeyFiles(staging, &key, key.Hostname())
	if err != nil {
		t.Fatal(err)
	}

	lockPublishDirectory(t, staging)

	err = publishMatch(staging, destination)
	if !retryablePublishError(err) {
		t.Fatalf("expected bounded failure while reader remains open: %v", err)
	}

	hostname, err := os.ReadFile(filepath.Join(staging, "hostname"))
	if err != nil || string(hostname) != key.Hostname()+"\n" {
		t.Fatalf("staging hostname: %q, %v", hostname, err)
	}

	_, err = os.Stat(destination)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed publication created destination: %v", err)
	}
}

func lockPublishDirectory(t *testing.T, path string) func() {
	t.Helper()

	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}

	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}

	closeReader := sync.OnceFunc(func() {
		closeError := syscall.CloseHandle(handle)
		if closeError != nil {
			t.Error(closeError)
		}
	})

	t.Cleanup(closeReader)

	return closeReader
}

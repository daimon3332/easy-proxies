//go:build windows
// +build windows

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var ol windows.Overlapped

	// Lock a huge region starting at offset 0 to effectively cover the whole file.
	return windows.LockFileEx(
		h,
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		0xFFFFFFFF,
		0xFFFFFFFF,
		&ol,
	)
}

func unlockFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var ol windows.Overlapped

	return windows.UnlockFileEx(
		h,
		0,
		0xFFFFFFFF,
		0xFFFFFFFF,
		&ol,
	)
}

func tryLockFile(f *os.File) (bool, error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		0xFFFFFFFF,
		0xFFFFFFFF,
		&ol,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

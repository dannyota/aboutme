package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var errUnsafeFile = errors.New("unsafe_file")

const maxPrivateFileBytes = 64 << 10

func atomicPrivateWrite(directory, name string, contents []byte) (err error) {
	if filepath.Base(name) != name {
		return errUnsafeFile
	}
	temporary, err := os.CreateTemp(directory, "."+name+"-")
	if err != nil {
		return errUnsafeFile
	}
	temporaryName := temporary.Name()
	defer func() {
		if removeErr := os.Remove(temporaryName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errUnsafeFile
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			return errUnsafeFile
		}
		return errUnsafeFile
	}
	if _, err := temporary.Write(contents); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			return errUnsafeFile
		}
		return errUnsafeFile
	}
	if err := temporary.Sync(); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			return errUnsafeFile
		}
		return errUnsafeFile
	}
	if err := temporary.Close(); err != nil {
		return errUnsafeFile
	}
	if err := os.Rename(temporaryName, filepath.Join(directory, name)); err != nil {
		return errUnsafeFile
	}
	return nil
}

func readPrivateFile(directory, name string) (contents []byte, err error) {
	if filepath.Base(name) != name {
		return nil, errUnsafeFile
	}
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errUnsafeFile
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			contents = nil
			err = errUnsafeFile
		}
	}()
	info, err := file.Stat()
	owner, ownerErr := privateFileOwner(os.Getuid())
	if err != nil || ownerErr != nil || !validPrivateFileInfo(info, owner) || info.Size() > maxPrivateFileBytes {
		return nil, errUnsafeFile
	}
	contents, err = io.ReadAll(io.LimitReader(file, maxPrivateFileBytes+1))
	if err != nil || len(contents) > maxPrivateFileBytes {
		return nil, errUnsafeFile
	}
	return contents, nil
}

func privateFileOwner(uid int) (uint32, error) {
	if uid < 0 || uint64(uid) > uint64(^uint32(0)) {
		return 0, errUnsafeFile
	}
	return uint32(uid), nil
}

func validPrivateFileInfo(info os.FileInfo, owner uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode() == 0o600 && stat.Uid == owner
}

package store

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// committed distinguishes a failed write from a completed replacement followed
// by a durability error. Both return an error; the latter must update the cache
// to reflect the file now visible to readers.
type writer func(path string, data []byte) (committed bool, err error)

func atomicWrite(path string, data []byte) (bool, error) {
	return atomicWriteWithReplace(path, data, replaceFile)
}

func atomicWriteWithReplace(path string, data []byte, replace func(string, string) error) (committed bool, err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".study-*.tmp")
	if err != nil {
		return false, err
	}
	temp := f.Name()
	defer func() {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
		if removeErr := os.Remove(temp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("清理临时文件：%w", removeErr))
		}
	}()
	n, err := f.Write(data)
	if err != nil {
		return false, err
	}
	if n != len(data) {
		return false, io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		return false, fmt.Errorf("同步残局文件：%w", err)
	}
	err = f.Close()
	f = nil
	if err != nil {
		return false, err
	}
	if err := replace(temp, path); err != nil {
		return false, fmt.Errorf("替换残局文件：%w", err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return true, fmt.Errorf("文件已替换，但目录同步失败：%w", err)
	}
	return true, nil
}

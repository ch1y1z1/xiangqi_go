//go:build !windows

package diagnostics

import "os"

func openSnapshotFile(path string) (*os.File, error) { return os.Open(path) }

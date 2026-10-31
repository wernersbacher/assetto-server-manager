//go:build !windows
// +build !windows

package skins

import "syscall"

// diskFree returns the bytes available to this process on the volume of path.
func diskFree(path string) (uint64, error) {
	var st syscall.Statfs_t

	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}

	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

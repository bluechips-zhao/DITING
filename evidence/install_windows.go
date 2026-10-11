//go:build windows

package evidence

import "golang.org/x/sys/windows"

// Directory handles cannot be flushed through os.File.Sync on Windows. Commit
// the already-flushed body with the native write-through rename instead.
func installBody(src, dest string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

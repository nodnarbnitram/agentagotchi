package pairing

import "os"

// mkdirOwnerOnly creates dir with 0700 and enforces the mode even if it
// already existed.
func mkdirOwnerOnly(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

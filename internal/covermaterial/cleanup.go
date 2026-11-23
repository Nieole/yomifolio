package covermaterial

import (
	"errors"
	"os"
	"regexp"
)

var materialKey = regexp.MustCompile(`^metadata-covers/[0-9a-f]{48}\.(jpg|png|webp|avif|img)$`)

// RemoveRetired removes only an exact server-generated immutable key after the
// caller has durably fenced publication. It never walks directories or follows links.
func RemoveRetired(thumbnails, key string) error {
	if !materialKey.MatchString(key) {
		return os.ErrInvalid
	}
	dir := RootDir(thumbnails)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return os.ErrInvalid
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err = root.Lstat(Directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return os.ErrInvalid
	}
	info, err = root.Lstat(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return os.ErrInvalid
	}
	err = root.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ValidKey excludes aliases, ordinary cache files and arbitrary user paths.
func ValidKey(key string) bool { return materialKey.MatchString(key) }

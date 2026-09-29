package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// writeAtomic replaces path with data via a temp file in the same directory and
// a rename.
//
// Both files this package owns are read on the next run and one of them is a
// secret. A direct write can be interrupted by a crash, a full disk or a
// Ctrl-C, and leaves a truncated file behind: an empty credentials.json reads
// as "not signed in" and a half-written one fails to parse. The rename is
// atomic on every platform the CLI ships to, so a reader either sees the whole
// old file or the whole new one.
//
// The temp file is chmodded before the rename, so the file is never visible
// under its real name with wider permissions than perm.
func writeAtomic(path string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := ensureDir(dir); err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmp := f.Name()
	// Every failure past this point must take the temp file with it; leaving
	// dotfiles in the config directory is how a directory slowly fills with
	// stale copies of a secret.
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()

	if err = f.Chmod(perm); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	// Sync before the rename: without it a power loss can leave the new name
	// pointing at a file whose contents never reached the disk.
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// removeFile deletes path, treating "already gone" as success so the delete is
// idempotent.
func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ensureDir creates the config directory when it is missing, and checks it when
// it is not.
func ensureDir(dir string) error {
	if err := vetDir(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return nil
}

// vetDir refuses a config directory that is not one this user should be
// trusting with a credential.
//
// A directory that does not exist yet is fine — it is a first run, and the
// caller is about to create it with 0700. An existing one must be a directory
// and must belong to the current user: whoever owns the directory can replace
// credentials.json and config.json inside it, which is enough to point the CLI
// at another API and hand it a key, or to hand the CLI a key of their own. The
// check is skipped on Windows, where file ownership is not reported the same way
// and the file modes this package sets are the whole defence anyway.
func vetDir(dir string) error {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot use %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	owner, ok := dirOwner(info)
	if !ok {
		return nil
	}
	if uid := os.Geteuid(); owner != uid {
		return fmt.Errorf("%s belongs to user %d, not you (%d): set $%s to a directory you own", dir, owner, uid, EnvConfigDir)
	}
	return nil
}

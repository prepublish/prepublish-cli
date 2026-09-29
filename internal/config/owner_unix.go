//go:build unix

package config

import (
	"io/fs"
	"syscall"
)

// dirOwner reports the uid that owns a config directory.
//
// It is a variable so the ownership rule can be tested where no second account
// exists to test it against: changing a directory's owner needs root, which a
// developer running the suite does not have.
var dirOwner = func(info fs.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

//go:build !unix

package config

import "io/fs"

// dirOwner reports no owner on the platforms that do not report one. Windows is
// the one the release archives cover: file ownership is not reported through
// the same interface there, the ownership rule is skipped in vetDir, and the
// file modes this package sets are the whole defence anyway.
var dirOwner = func(fs.FileInfo) (int, bool) { return 0, false }

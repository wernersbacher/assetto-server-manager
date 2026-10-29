// Package plainname validates names that end up in file system paths.
package plainname

import (
	"path/filepath"
	"strings"
)

// Valid checks that name is a single, plain file or directory name which is safe to join onto a base
// path: it must not be empty, "." or "..", and must not contain path separators or other characters with a
// special meaning in a path. It is used for URL parameters and form values that end up in file system paths.
func Valid(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		filepath.Base(name) == name &&
		!strings.ContainsAny(name, "/\\:\x00")
}

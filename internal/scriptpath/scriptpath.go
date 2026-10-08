// Package scriptpath maps workflow and form names to the file paths their
// scripts are stored under in a repository checkout. The server's code file
// store and the command-line client share it so both lay files out the same
// way.
package scriptpath

import (
	"path"
	"regexp"
	"strings"
)

var unsafeSegment = regexp.MustCompile(`[^\pL\pN._-]+`)

// Relative returns the slash-separated path of a script relative to the
// repository root, e.g. "workflow/sales/notify.js".
func Relative(kind, databaseName, name string) string {
	return path.Join(kind, Segment(databaseName), Segment(name)+".js")
}

// Segment turns a database, workflow, or form name into a single safe path
// segment. The mapping is lossy, so a path cannot be turned back into a name.
func Segment(value string) string {
	segment := unsafeSegment.ReplaceAllString(strings.TrimSpace(value), "-")
	segment = strings.Trim(segment, ".-")
	if segment == "" {
		return "unnamed"
	}
	return segment
}

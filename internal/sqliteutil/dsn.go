// Package sqliteutil centralizes the DSN options every read-write SQLite
// handle in autable must use.
package sqliteutil

// DSN appends the connection options for read-write handles:
// WAL journaling with synchronous=NORMAL so each committed transaction does
// not force a full fsync, and a busy timeout so concurrent writers wait
// instead of failing with SQLITE_BUSY.
func DSN(path string) string {
	return path + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
}

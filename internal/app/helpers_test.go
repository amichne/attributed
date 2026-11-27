package app

import (
	"strings"
	"testing"
	"time"
)

// appHead returns the HEAD commit of the repository at root.
func appHead(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
}

// zeroTime is an Env clock that always reports the zero time.
func zeroTime() (zero time.Time) {
	return zero
}

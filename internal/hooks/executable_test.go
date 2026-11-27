package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// packageManagerLayout builds a versioned install directory and a stable
// symlink to it, the way Homebrew links a formula's binary into bin/.
func packageManagerLayout(t *testing.T) (stable, versioned string) {
	t.Helper()
	root := t.TempDir()
	versioned = filepath.Join(root, "Cellar", "git-attributed", "0.1.0", "bin", "git-attributed")
	if err := os.MkdirAll(filepath.Dir(versioned), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versioned, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stable = filepath.Join(root, "bin", "git-attributed")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versioned, stable); err != nil {
		t.Fatal(err)
	}
	return stable, versioned
}

func TestInvokedExecutableKeepsTheStableSymlink(t *testing.T) {
	t.Parallel()
	stable, versioned := packageManagerLayout(t)
	resolved := func() (string, error) { return versioned, nil }
	noLookup := func(string) (string, error) { return "", errors.New("unused") }

	if got, ok := invokedExecutable(stable, noLookup, resolved); !ok || got != stable {
		t.Errorf("invoked by path = %q, %t; want %q", got, ok, stable)
	}
	lookup := func(name string) (string, error) {
		if name != "git-attributed" {
			t.Fatalf("lookPath(%q)", name)
		}
		return stable, nil
	}
	if got, ok := invokedExecutable("git-attributed", lookup, resolved); !ok || got != stable {
		t.Errorf("invoked through PATH = %q, %t; want %q", got, ok, stable)
	}
}

func TestInvokedExecutableRejectsAnotherFile(t *testing.T) {
	t.Parallel()
	stable, _ := packageManagerLayout(t)
	other := filepath.Join(t.TempDir(), "git-attributed")
	if err := os.WriteFile(other, []byte("other"), 0o755); err != nil {
		t.Fatal(err)
	}
	running := func() (string, error) { return other, nil }
	noLookup := func(string) (string, error) { return "", errors.New("unused") }
	if got, ok := invokedExecutable(stable, noLookup, running); ok {
		t.Errorf("a different file was accepted: %q", got)
	}
	missing := func(string) (string, error) { return "", errors.New("not found") }
	if got, ok := invokedExecutable("git-attributed", missing, running); ok {
		t.Errorf("a failed PATH lookup was accepted: %q", got)
	}
}

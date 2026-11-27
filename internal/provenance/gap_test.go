package provenance

import (
	"strings"
	"testing"

	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/store"
)

// TestAnnotateResumesAfterMissedCommits covers commits that the hooks never
// annotated, for example while git-attributed was not installed. The next
// annotation resumes with a warning, the missed commit keeps no note, and
// later commits annotate as usual.
func TestAnnotateResumesAfterMissedCommits(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "a.txt", "one\n")
	first := commit(t, root, "one")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "a.txt", "one\ntwo\n")
	missed := commit(t, root, "two, not annotated")
	write(t, root, "a.txt", "one\ntwo\nthree\n")
	resumedHead := commit(t, root, "three")

	result, err := Annotate(repo)
	if err != nil {
		t.Fatalf("Annotate after a gap = %v", err)
	}
	if result.Commit != resumedHead || result.Skipped || result.Noop {
		t.Fatalf("Annotate after a gap = %+v", result)
	}
	if !containsWarning(result.Warnings, "resuming attribution after commits that were not annotated") {
		t.Fatalf("warnings = %q", result.Warnings)
	}
	for commit, want := range map[string]bool{first: true, missed: false, resumedHead: true} {
		if _, found, err := repo.ReadNote(commit); err != nil || found != want {
			t.Errorf("note on %s found = %t, %v; want %t", commit, found, err, want)
		}
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil || state.LastAnnotatedCommit != resumedHead {
		t.Fatalf("state boundary = %q, %v; want %s", state.LastAnnotatedCommit, err, resumedHead)
	}

	write(t, root, "a.txt", "one\ntwo\nthree\nfour\n")
	next := commit(t, root, "four")
	result, err = Annotate(repo)
	if err != nil || result.Commit != next || containsWarning(result.Warnings, "resuming") {
		t.Fatalf("Annotate after resuming = %+v, %v", result, err)
	}
}

func containsWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

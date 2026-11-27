package interop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/model"
	"github.com/amichne/attributed/internal/notes"
)

// The fixture is a note that the pinned upstream git-byline binary wrote to
// refs/notes/byline for a commit holding fixtureContent. Blob IDs depend
// only on content, so any repository with that file reproduces the blob.
const fixtureContent = "human base\nai alpha\nai beta\n"

func bylineFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "git-byline-note.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// bylineRepo commits content as app.txt and attaches note to that commit
// under refs/notes/byline.
func bylineRepo(t *testing.T, content string, note []byte) (*gitcmd.Repo, string) {
	t.Helper()
	root := interopRepo(t)
	writeInteropFile(t, root, "app.txt", content)
	head := commitInterop(t, root, "ai lines")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if note != nil {
		if err := repo.WriteNoteRef(BylineNotesRef, head, note); err != nil {
			t.Fatal(err)
		}
	}
	return repo, head
}

func attributedNote(t *testing.T, repo *gitcmd.Repo, commit string) ([]byte, bool) {
	t.Helper()
	data, found, err := repo.ReadNoteRef("refs/notes/attributed", commit)
	if err != nil {
		t.Fatal(err)
	}
	return data, found
}

func TestImportBylineCopiesAnUpstreamNote(t *testing.T) {
	t.Parallel()
	fixture := bylineFixture(t)
	repo, head := bylineRepo(t, fixtureContent, fixture)

	result, err := ImportByline(repo, "", false)
	if err != nil || result.Imported != 1 || result.Skipped != 0 || len(result.Warnings) != 0 {
		t.Fatalf("ImportByline() = %+v, %v", result, err)
	}
	data, found := attributedNote(t, repo, head)
	if !found || !bytes.Equal(bytes.TrimSuffix(data, []byte{'\n'}), bytes.TrimSuffix(fixture, []byte{'\n'})) {
		t.Fatalf("attributed note = %q, want the upstream bytes", data)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	ranges := note.Files["app.txt"].Ranges
	if len(ranges) != 2 || ranges[0].Author != model.AuthorHuman ||
		ranges[1].Author != model.AuthorAI || ranges[1].Agent != "claude" || ranges[1].Model != "fixture-model" {
		t.Fatalf("imported ranges = %+v", ranges)
	}

	again, err := ImportByline(repo, "", false)
	if err != nil || again.Imported != 0 || again.Skipped != 0 || len(again.Warnings) != 0 {
		t.Fatalf("repeated ImportByline() = %+v, %v, want no change", again, err)
	}
}

func TestImportBylineDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	repo, head := bylineRepo(t, fixtureContent, bylineFixture(t))
	result, err := ImportByline(repo, "", true)
	if err != nil || result.Imported != 1 {
		t.Fatalf("dry-run ImportByline() = %+v, %v", result, err)
	}
	if _, found := attributedNote(t, repo, head); found {
		t.Fatal("dry run wrote an attributed note")
	}
}

func TestImportBylineSkipsNotesThatDoNotMatchTheCommit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		note    []byte
		warning string
	}{
		{name: "different blob", content: "other content\n", note: bylineFixture(t), warning: "does not match commit"},
		{name: "malformed note", content: fixtureContent, note: []byte("{not json"), warning: "skipped git-byline note"},
		{
			name:    "unsupported version",
			content: fixtureContent,
			note:    []byte(`{"version":99,"files":{}}`),
			warning: "skipped git-byline note",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo, head := bylineRepo(t, test.content, test.note)
			result, err := ImportByline(repo, "", false)
			if err != nil || result.Imported != 0 || result.Skipped != 1 ||
				len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], test.warning) {
				t.Fatalf("ImportByline() = %+v, %v; want one skip mentioning %q", result, err, test.warning)
			}
			if _, found := attributedNote(t, repo, head); found {
				t.Fatal("a rejected note reached refs/notes/attributed")
			}
		})
	}
}

func TestImportBylineNeverReplacesADifferentNote(t *testing.T) {
	t.Parallel()
	repo, head := bylineRepo(t, fixtureContent, bylineFixture(t))
	blob, exists, err := repo.BlobID(head, "app.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	existing, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{"app.txt": {
			Blob:   blob,
			Ranges: []model.Range{{Start: 1, End: 3, Attribution: model.Attribution{Author: model.AuthorUntracked}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNoteRef("refs/notes/attributed", head, existing); err != nil {
		t.Fatal(err)
	}
	result, err := ImportByline(repo, "", false)
	if err != nil || result.Imported != 0 || result.Skipped != 1 ||
		len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "existing attributed note is different") {
		t.Fatalf("ImportByline() = %+v, %v", result, err)
	}
	data, _ := attributedNote(t, repo, head)
	if !bytes.Equal(data, existing) && !bytes.Equal(bytes.TrimSuffix(data, []byte{'\n'}), existing) {
		t.Fatalf("existing attributed note changed to %q", data)
	}
}

func TestImportBylineWalksARangeAndIgnoresCommitsWithoutNotes(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "app.txt", "human base\n")
	first := commitInterop(t, root, "base")
	writeInteropFile(t, root, "app.txt", fixtureContent)
	second := commitInterop(t, root, "ai lines")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNoteRef(BylineNotesRef, second, bylineFixture(t)); err != nil {
		t.Fatal(err)
	}
	result, err := ImportByline(repo, "HEAD", false)
	if err != nil || result.Imported != 1 || result.Skipped != 0 {
		t.Fatalf("ImportByline(HEAD) = %+v, %v", result, err)
	}
	if _, found := attributedNote(t, repo, first); found {
		t.Fatal("a commit without a git-byline note gained an attributed note")
	}
	if _, found := attributedNote(t, repo, second); !found {
		t.Fatal("the noted commit was not imported")
	}
}

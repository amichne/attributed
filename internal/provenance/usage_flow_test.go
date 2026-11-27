package provenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/model"
	"github.com/amichne/attributed/internal/notes"
	"github.com/amichne/attributed/internal/preset"
	"github.com/amichne/attributed/internal/store"
)

// usageTurn is a transcript line that carries token usage. Capture must not
// read it: Attributed removed token accounting from its producers.
const usageTurn = `{"type":"assistant","message":{"id":"msg_a","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":5,"cache_read_input_tokens":7}}}` + "\n"

func TestCaptureRecordsNoTokenUsage(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(usageTurn), 0o600); err != nil {
		t.Fatal(err)
	}
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
		TranscriptPath: transcriptPath,
	}
	write(t, root, "file.txt", "base\nai-one\n")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if result, err := Capture(repo, event, now); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture() = %+v, %v", result, err)
	}
	records, _, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Usage != nil {
		t.Fatalf("records = %+v, want one checkpoint without usage", records)
	}

	git(t, root, "add", "file.txt")
	git(t, root, "commit", "-m", "ai edit")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("ReadNote() = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	if !ok || session.HasTokenUsage() {
		t.Fatalf("note session = %+v, %t; want a session without token usage", session, ok)
	}
}

// TestFoldSessionUsageKeepsOlderCheckpoints covers pending checkpoints that
// an older producer wrote with usage. Annotation still folds them, capped.
func TestFoldSessionUsageKeepsOlderCheckpoints(t *testing.T) {
	t.Parallel()
	sessions := sessionMetrics{}
	foldSessionUsage(sessions, []model.Checkpoint{
		{
			Type: model.AuthorAI, Agent: "droid", Session: "session-1",
			Usage: &model.CheckpointUsage{MsgID: "first", TokensIn: model.MaxCheckpointUsageTokens - 1},
		},
		{
			Type: model.AuthorAI, Agent: "droid", Session: "session-1",
			Usage: &model.CheckpointUsage{MsgID: "second", TokensIn: 2},
		},
	})
	session := sessions[model.NoteSessionKey("droid", "session-1")]
	if session.TokensIn != model.MaxCheckpointUsageTokens {
		t.Fatalf("tokens in = %d, want cap %d", session.TokensIn, model.MaxCheckpointUsageTokens)
	}
}

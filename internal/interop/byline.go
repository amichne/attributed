package interop

import (
	"errors"
	"fmt"

	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/notes"
)

// BylineNotesRef is the canonical note namespace of upstream git-byline.
// Attributed shares its note format, versions 1 through 4, and writes its
// own notes under refs/notes/attributed instead.
const BylineNotesRef = "refs/notes/byline"

// ImportByline copies git-byline notes into refs/notes/attributed. Each
// note must decode as a supported note version and match its commit's
// blobs and line counts. The original bytes are copied, so the note keeps
// its version. An existing different attributed note is never replaced.
func ImportByline(repo *gitcmd.Repo, revisionRange string, dryRun bool) (ImportResult, error) {
	if repo == nil {
		return ImportResult{}, errors.New("interop repository is nil")
	}
	commits, err := importCommits(repo, revisionRange)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{}
	for _, commit := range commits {
		data, found, err := repo.ReadNoteRef(BylineNotesRef, commit)
		if err != nil {
			return result, fmt.Errorf("read git-byline note on %s: %w", commit, err)
		}
		if !found {
			continue
		}
		note, err := notes.Decode(data)
		if err == nil {
			err = validateNoteFiles(repo, commit, note)
		}
		if err != nil {
			result.Skipped++
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped git-byline note on %s: %v", commit, err))
			continue
		}
		existing, exists, err := repo.ReadNoteRef("refs/notes/attributed", commit)
		if err != nil {
			return result, fmt.Errorf("read attributed note on %s: %w", commit, err)
		}
		if exists {
			if !sameNoteBytes(existing, data) {
				result.Skipped++
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipped %s: existing attributed note is different", commit))
			}
			continue
		}
		if !dryRun {
			if err := repo.WriteNoteRef("refs/notes/attributed", commit, data); err != nil {
				if isDifferentNoteConflict(err) {
					result.Skipped++
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("skipped %s: existing attributed note is different", commit))
					continue
				}
				return result, fmt.Errorf("write attributed note on %s: %w", commit, err)
			}
		}
		result.Imported++
	}
	return result, nil
}

package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/model"
	"github.com/amichne/attributed/internal/notes"
	"github.com/amichne/attributed/internal/provenance"
)

func TestProductCommandFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	code, stdout, stderr, err := appRun(root, now, nil, "annotate")
	if code != ExitSuccess || err != nil || !strings.Contains(stdout, "annotated ") || stderr != "" {
		t.Fatalf("annotate = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "annotate", "--drop-stranded")
	if code != ExitSuccess || err != nil || !strings.Contains(stdout, "already annotated ") || stderr != "" {
		t.Fatalf("annotate drop stranded = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	humanPayload := `{"session_id":"s","tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, stdout, stderr, err = appRun(root, now, strings.NewReader(humanPayload),
		"checkpoint", "droid", "--type", "human", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("human checkpoint = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	appWrite(t, root, "file.txt", "base\nai\n")
	code, _, stderr, err = appRun(root, now.Add(time.Second), strings.NewReader(humanPayload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("ai checkpoint = %d, %q, %v", code, stderr, err)
	}
	appWrite(t, root, "second.txt", "manual\n")
	appCommit(t, root, "ai")
	if code, _, _, err = appRun(root, now, nil, "annotate"); code != ExitSuccess || err != nil {
		t.Fatalf("second annotate = %d, %v", code, err)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "blame", "file.txt", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame = %d, %q, %v", code, stderr, err)
	}
	var blame provenance.BlameResult
	if err := json.Unmarshal([]byte(stdout), &blame); err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 2 || blame.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("blame = %+v", blame)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "status", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("status = %d, %q, %v", code, stderr, err)
	}
	var status provenance.StatusResult
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatal(err)
	}
	if status.Head == "" || status.Head != status.LastAnnotatedCommit {
		t.Fatalf("status = %+v", status)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "blame", "file.txt")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "ai:droid/unknown") {
		t.Fatalf("text blame = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "status")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "Pending checkpoints: 0") {
		t.Fatalf("text status = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestBlameTextRendersHumanOverrideMetadata(t *testing.T) {
	t.Parallel()
	root, repo, head, blob := appRepoWithCommittedFile(t, "file.txt", "human replacement\n")
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{
						Author: model.AuthorHumanOverride, Agent: "droid", Model: "model",
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, nil, "blame", "file.txt")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "human-override:droid/model") {
		t.Fatalf("blame = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestHookCommands(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"install-hooks", "--agent", "droid", "--git", "--project")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "updated ") ||
		!strings.Contains(stdout, "attribution notes will be pushed automatically") {
		t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".factory", "hooks.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "pre-push")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err = appRun(root, time.Time{}, nil,
		"uninstall", "--agent", "droid", "--git", "--project")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "updated ") {
		t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestTemplateHookCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	install := []string{"install-hooks", "--agent", "none", "--git", "--template", "--local-notes"}
	uninstall := []string{"uninstall", "--agent", "none", "--git", "--template"}

	stdout := runTemplateCommand(t, root, install...)
	if !strings.Contains(stdout, "updated global git config init.templateDir") {
		t.Fatalf("template install status = %q, want the config update line", stdout)
	}
	if strings.Contains(stdout, "hooks already installed") {
		t.Fatalf("template install status = %q, config change must suppress the no-op line", stdout)
	}
	stdout = runTemplateCommand(t, root, install...)
	if stdout != "hooks already installed\n" {
		t.Fatalf("template reinstall status = %q, want only the no-op line", stdout)
	}
	stdout = runTemplateCommand(t, root, uninstall...)
	if !strings.Contains(stdout, "removed global git config init.templateDir") {
		t.Fatalf("template uninstall status = %q, want the config removal line", stdout)
	}
	if strings.Contains(stdout, "no managed hooks found") {
		t.Fatalf("template uninstall status = %q, config change must suppress the no-op line", stdout)
	}
	stdout = runTemplateCommand(t, root, uninstall...)
	if stdout != "no managed hooks found\n" {
		t.Fatalf("template re-uninstall status = %q, want only the no-op line", stdout)
	}
}

func TestProductCommandUsageAndFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	tests := []struct {
		name  string
		input string
		args  []string
		code  int
	}{
		{"checkpoint missing preset", "", []string{"checkpoint"}, ExitUsage},
		{"checkpoint missing type", `{}`, []string{"checkpoint", "droid", "--hook-input", "stdin"}, ExitUsage},
		{"checkpoint invalid input source", `{}`, []string{"checkpoint", "droid", "--type", "ai"}, ExitUsage},
		{"checkpoint invalid owner", `{}`, []string{"checkpoint", "droid", "--managed-by", "other", "--type", "ai", "--hook-input", "stdin"}, ExitUsage},
		{"checkpoint unknown flag", `{}`, []string{"checkpoint", "droid", "--bad"}, ExitUsage},
		{"agent v1 explicit type", `{}`, []string{"checkpoint", "agent-v1", "--type", "ai", "--hook-input", "stdin"}, ExitUsage},
		{"annotate argument", "", []string{"annotate", "extra"}, ExitUsage},
		{"annotate unknown flag", "", []string{"annotate", "--bad"}, ExitUsage},
		{"blame missing file", "", []string{"blame"}, ExitUsage},
		{"blame unknown flag", "", []string{"blame", "--bad", "file"}, ExitUsage},
		{"status argument", "", []string{"status", "extra"}, ExitUsage},
		{"local notes without git", "", []string{"install-hooks", "--agent", "droid", "--local-notes"}, ExitUsage},
		{"install conflicting scope", "", []string{"install-hooks", "--user", "--project"}, ExitUsage},
		{"install unknown agent", "", []string{"install-hooks", "--agent", "other"}, ExitUsage},
		{"template without git", "", []string{"install-hooks", "--agent", "none", "--template"}, ExitUsage},
		{"template with project agents", "", []string{"install-hooks", "--git", "--template"}, ExitUsage},
		{"uninstall template without git", "", []string{"uninstall", "--agent", "none", "--template"}, ExitUsage},
		{"uninstall template with project agents", "", []string{"uninstall", "--git", "--template"}, ExitUsage},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			code, _, _, err := appRun(root, time.Time{}, strings.NewReader(test.input), test.args...)
			if code != test.code || err == nil {
				t.Fatalf("Run(%v) = %d, %v, want %d, error", test.args, code, err, test.code)
			}
		})
	}
	for _, args := range [][]string{{"blame", "-h"}, {"status", "--help"}, {"install-hooks", "-h"}, {"uninstall", "--help"}} {
		code, stdout, stderr, err := appRun(root, time.Time{}, nil, args...)
		if code != ExitSuccess || err != nil || stdout == "" || stderr != "" {
			t.Fatalf("Run(%v) help = %d, %q, %q, %v", args, code, stdout, stderr, err)
		}
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, strings.NewReader("{}"),
		"checkpoint", "droid", "-h")
	if code != ExitSuccess || err != nil || stdout == "" || stderr != "" {
		t.Fatalf("checkpoint help = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, _, _, err = appRun(t.TempDir(), time.Time{}, nil, "status")
	if code != ExitFailure || err == nil {
		t.Fatalf("status outside repo = %d, %v", code, err)
	}
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, stdout, stderr, err = appRun(t.TempDir(), time.Time{}, strings.NewReader(payload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("checkpoint outside repo = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	empty := appRepo(t)
	code, stdout, stderr, err = appRun(empty, time.Time{}, nil, "status")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "HEAD: (none)") {
		t.Fatalf("empty status = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestCheckpointInputDeadline(t *testing.T) {
	oldTimeout := checkpointInputTimeoutNanos.Swap(int64(10 * time.Millisecond))
	defer checkpointInputTimeoutNanos.Store(oldTimeout)

	reader, writer := io.Pipe()
	defer writer.Close()
	root := t.TempDir()
	done := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, _, _, err := appRun(root, time.Time{}, reader,
			"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
		done <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()
	select {
	case result := <-done:
		if result.code != ExitFailure || result.err == nil ||
			!strings.Contains(result.err.Error(), "timed out") {
			t.Fatalf("checkpoint deadline = %d, %v", result.code, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("checkpoint input did not time out")
	}
}

func TestCheckpointModelResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		transcript string
		sidecar    string
		want       string
	}{
		{
			name:       "transcript model",
			transcript: `{"type":"message","message":{"role":"assistant","modelId":"droid-test-model"}}` + "\n",
			want:       "ai:droid/droid-test-model",
		},
		{
			name:       "sidecar fallback",
			transcript: "{\"message\":{\"role\":\"user\"}}\n",
			sidecar:    `{"model":"sidecar-model"}`,
			want:       "ai:droid/sidecar-model",
		},
		{
			name: "unresolved stays unknown",
			want: "ai:droid/unknown",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runModelResolutionCase(t, test.transcript, test.sidecar, test.want)
		})
	}
}

func runModelResolutionCase(t *testing.T, transcript, sidecar, want string) {
	t.Helper()
	dir := t.TempDir()
	if transcript != "" {
		if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(transcript), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if sidecar != "" {
		if err := os.WriteFile(filepath.Join(dir, "session.settings.json"), []byte(sidecar), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	// Backslashes in a Windows temp path are not JSON escapes.
	payload := `{"session_id":"s","tool_name":"Edit","transcript_path":"` + filepath.ToSlash(filepath.Join(dir, "session.jsonl")) +
		`","tool_input":{"file_path":"file.txt"}}`
	stdout := runCheckpointBlameFlow(t, root, now, "droid", payload, payload)
	if !strings.Contains(stdout, want) {
		t.Fatalf("blame %q does not contain %q", stdout, want)
	}
}

func TestWindsurfCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	prePayload := `{"agent_action_name":"pre_write_code","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"file_path":"file.txt"}}`
	postPayload := `{"agent_action_name":"post_write_code","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"file_path":"file.txt"}}`
	stdout := runCheckpointBlameFlow(t, root, now, "portable-windsurf", prePayload, postPayload)
	if !strings.Contains(stdout, "ai:windsurf/Claude Sonnet 4") {
		t.Fatalf("blame %q does not contain the windsurf model", stdout)
	}
}

func TestOpenCodeCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	prePayload := `{"target":"opencode","event":"pre-tool-use","tool":"edit","args":{"filePath":"file.txt"},"sessionID":"session-1","callID":"edit-1"}`
	postPayload := `{"target":"opencode","event":"post-tool-use","tool":"edit","args":{"filePath":"file.txt"},"sessionID":"session-1","callID":"edit-1"}`
	stdout := runCheckpointBlameFlow(t, root, now, "portable-opencode", prePayload, postPayload)
	if !strings.Contains(stdout, "ai:opencode/unknown") {
		t.Fatalf("blame %q does not contain unknown OpenCode model attribution", stdout)
	}
}

func TestOpenCodeApplyPatchCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	patchText := `*** Begin Patch\n*** Update File: file.txt\n@@\n base\n+ai\n*** End Patch`
	prePayload := `{"target":"opencode","event":"pre-tool-use","tool":"apply_patch","args":{"patchText":"` + patchText + `"},"sessionID":"session-1","callID":"patch-1"}`
	postPayload := `{"target":"opencode","event":"post-tool-use","tool":"apply_patch","args":{"patchText":"` + patchText + `"},"sessionID":"session-1","callID":"patch-1"}`
	stdout := runCheckpointBlameFlow(t, root, now, "portable-opencode", prePayload, postPayload)
	if !strings.Contains(stdout, "ai:opencode/unknown") {
		t.Fatalf("blame %q does not contain unknown OpenCode patch attribution", stdout)
	}
}

func TestOpenCodeShellCheckpointFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	prePayload := `{"target":"opencode","event":"pre-tool-use","tool":"bash","args":{"command":"printf shell"},"sessionID":"session-1","callID":"shell-1"}`
	postPayload := `{"target":"opencode","event":"post-tool-use","tool":"bash","args":{"command":"printf shell"},"sessionID":"session-1","callID":"shell-1"}`
	if code, _, stderr, err := appRun(root, now, strings.NewReader(prePayload),
		"checkpoint", "portable-opencode", "--type", "human", "--hook-input", "stdin"); code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("shell pre checkpoint = %d, %q, %v", code, stderr, err)
	}
	appWrite(t, root, "file.txt", "base\nshell\n")
	if code, _, stderr, err := appRun(root, now.Add(time.Second), strings.NewReader(postPayload),
		"checkpoint", "portable-opencode", "--type", "ai", "--hook-input", "stdin"); code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("shell post checkpoint = %d, %q, %v", code, stderr, err)
	}
	appCommit(t, root, "shell edit")
	if code, _, _, err := appRun(root, now, nil, "annotate"); code != ExitSuccess || err != nil {
		t.Fatalf("annotate = %d, %v", code, err)
	}
	if code, stdout, stderr, err := appRun(root, now, nil, "blame", "file.txt"); code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame = %d, %q, %v", code, stderr, err)
	} else if !strings.Contains(stdout, "ai:opencode/unknown") {
		t.Fatalf("blame %q does not contain unknown OpenCode shell attribution", stdout)
	}
}

func appRun(root string, now time.Time, input io.Reader, args ...string) (int, string, string, error) {
	if input == nil {
		input = strings.NewReader("")
	}
	var stdout, stderr bytes.Buffer
	env := &Env{
		Stdin:  input,
		Stdout: &stdout,
		Stderr: &stderr,
		Dir:    root,
		Now:    func() time.Time { return now },
	}
	code, err := Run(args, env)
	return code, stdout.String(), stderr.String(), err
}

func appRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	appGit(t, root, "init", "-b", "main")
	appGit(t, root, "config", "user.name", "Test User")
	appGit(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func appWrite(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appCommit(t *testing.T, root, message string) {
	t.Helper()
	appGit(t, root, "add", "-A")
	appGit(t, root, "commit", "-m", message)
}

func appGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// appRepoWithCommittedFile commits one file and returns the repository, its
// HEAD, and the blob ID of the committed content.
func appRepoWithCommittedFile(t *testing.T, path, content string) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root := appRepo(t)
	appWrite(t, root, path, content)
	appCommit(t, root, "content")
	head := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, path)
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	return root, repo, head, blob
}

// runTemplateCommand asserts one successful template-scope hook command and
// returns its stdout.
func runTemplateCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	code, stdout, stderr, err := appRun(root, time.Time{}, nil, args...)
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("Run(%v) = %d, %q, %q, %v", args, code, stdout, stderr, err)
	}
	return stdout
}

// runCheckpointBlameFlow records a human and an AI checkpoint for the preset
// with the two payloads, commits, annotates, and returns the blame text.
func runCheckpointBlameFlow(t *testing.T, root string, now time.Time, preset, humanPayload, aiPayload string) string {
	t.Helper()
	code, _, stderr, err := appRun(root, now, strings.NewReader(humanPayload),
		"checkpoint", preset, "--type", "human", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("human checkpoint = %d, %q, %v", code, stderr, err)
	}
	appWrite(t, root, "file.txt", "base\nai\n")
	code, _, stderr, err = appRun(root, now.Add(time.Second), strings.NewReader(aiPayload),
		"checkpoint", preset, "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("ai checkpoint = %d, %q, %v", code, stderr, err)
	}
	appCommit(t, root, "ai")
	if code, _, _, err = appRun(root, now, nil, "annotate"); code != ExitSuccess || err != nil {
		t.Fatalf("annotate = %d, %v", code, err)
	}
	code, stdout, stderr, err := appRun(root, now, nil, "blame", "file.txt")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame = %d, %q, %v", code, stderr, err)
	}
	return stdout
}

func TestAnnotatePrintsSkippedDuringRebase(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	appGit(t, root, "checkout", "-q", "-b", "feat")
	appWrite(t, root, "feat.txt", "feat\n")
	appCommit(t, root, "feat")
	appGit(t, root, "checkout", "-q", "main")
	appWrite(t, root, "main.txt", "main\n")
	appCommit(t, root, "main work")
	appGit(t, root, "checkout", "-q", "feat")
	appGit(t, root, "rebase", "-q", "main")

	code, stdout, stderr, err := appRun(root, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), nil, "annotate")
	if code != ExitSuccess || err != nil {
		t.Fatalf("annotate during rebase = %d, %v", code, err)
	}
	if !strings.Contains(stdout, "skipped ") || strings.Contains(stdout, "annotated ") {
		t.Fatalf("annotate output = %q, want skipped", stdout)
	}
	if !strings.Contains(stderr, "rebase replay") {
		t.Fatalf("annotate warnings = %q, want rebase replay hint", stderr)
	}
}

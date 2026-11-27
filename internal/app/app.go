// Package app implements git-attributed command dispatch and handlers.
//
// Run is the single entry point used by cmd/git-attributed. Handlers receive
// their input and output streams through Env and never touch os.Stdin,
// os.Stdout, or os.Stderr directly, which keeps every behavior testable
// without spawning processes.
package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/amichne/attributed"
	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/model"
	"github.com/amichne/attributed/internal/version"
)

// Exit codes returned by Run. Usage errors are separated from operational
// failures so scripts can react precisely, following the convention of git
// and the standard flag package.
const (
	// ExitSuccess reports a completed command.
	ExitSuccess = 0
	// ExitFailure reports an operational failure.
	ExitFailure = 1
	// ExitUsage reports a usage error: an unknown command, an unknown
	// flag, or wrong arguments.
	ExitUsage = 2
)

// Env carries the streams a command reads and writes.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Dir    string
	Now    func() time.Time
}

// command is a single git-attributed subcommand.
type command struct {
	name  string                                                 // invocation name, as typed by the user
	short string                                                 // one-line description shown in the command list
	usage string                                                 // full usage text, printed by help
	run   func(env *Env, c *command, args []string) (int, error) // command handler
}

// commands returns the command registry in help order.
func commands() []*command {
	return []*command{
		{
			name:  "checkpoint",
			short: "record a human or AI edit snapshot",
			usage: "Usage: git-attributed checkpoint <droid|claude|agent-v1|portable-AGENT> [--type human|ai] --hook-input stdin\n\n" +
				"Read one agent hook event from stdin and record allowed file snapshots.",
			run: runCheckpoint,
		},
		{
			name:  "annotate",
			short: "write attribution for HEAD to git notes",
			usage: "Usage: git-attributed annotate [--drop-stranded]\n\n" +
				"Replay pending checkpoints and annotate the current commit.\n" +
				"Checkpoints recorded in another branch context park with a\n" +
				"warning and resume when branch and base return; annotation\n" +
				"never deletes them. Use 'git-attributed recover' to preview them.\n" +
				"--drop-stranded also discards parked checkpoints whose base\n" +
				"no branch can reach, after ordinary annotation validates.",
			run: runAnnotate,
		},
		{
			name:  "recover",
			short: "preview or resolve stranded checkpoints",
			usage: "Usage: git-attributed recover [--drop [--all]] [--json]\n\n" +
				"Preview parked checkpoint branch contexts, object availability,\n" +
				"and branches that still reach each base. Default changes no state.\n" +
				"Use --drop to discard only unreachable checkpoints and retry\n" +
				"annotation. --drop --all discards every parked checkpoint, also\n" +
				"those a branch still reaches; their attribution evidence is lost.",
			run: runRecover,
		},
		{
			name:  "blame",
			short: "show line-level attribution",
			usage: "Usage: git-attributed blame [--json] [--color=auto|always|never] <file>\n\n" +
				"Show human, AI, human-override, or untracked attribution for every line at HEAD.\n" +
				"Human lines carry the identity of the commit author that introduced them.\n" +
				"The file argument resolves against the working directory first, then\n" +
				"the repository root.",
			run: runBlame,
		},
		{
			name:  "status",
			short: "show repository attribution state",
			usage: "Usage: git-attributed status [--json]\n\n" +
				"Show pending checkpoints, retained snapshots, and annotation state.",
			run: runStatus,
		},
		{
			name:  "verify",
			short: "verify attribution notes",
			usage: "Usage: git-attributed verify [<rev-range>] [--deep] [--json]\n\n" +
				"Check attribution notes, blob identity, and range coverage.\n" +
				"--deep reads blobs to verify exact line counts.",
			run: runVerify,
		},
		{
			name:  "rewrite",
			short: "preserve attribution across Git rewrites",
			usage: "Usage: git-attributed rewrite --mode <post-rewrite|post-checkout|post-merge|ref-txn|stash-apply> --hook-input stdin [hook arguments]\n\n" +
				"Reproject attribution for Git history and worktree transitions.",
			run: runRewrite,
		},
		{
			name:  "merge-notes",
			short: "merge fetched remote attribution notes",
			usage: "Usage: git-attributed merge-notes [--remote NAME]\n\n" +
				"Merge the attribution notes that the managed pre-push hook fetched\n" +
				"into " + gitcmd.RemoteNotesRef + ". Remote notes may only add\n" +
				"notes: local notes that are behind fast-forward and diverged notes\n" +
				"merge. When the remote side changed or removed a local note, or both\n" +
				"sides wrote different notes for one commit, nothing changes, the\n" +
				"manual merge steps are printed, and the command exits 1. A fetched\n" +
				"history that holds anything besides notes, or a note over 16 MiB,\n" +
				"also changes nothing and exits 1. --remote only names the remote in\n" +
				"the manual steps; the command never contacts a remote.",
			run: runMergeNotes,
		},
		{
			name:  "export",
			short: "export attribution to an interop format",
			usage: "Usage: git-attributed export --format gitai|agent-trace [--commit REV] [--output FILE]\n\n" +
				"Export the selected commit's attribution without replacing output files.",
			run: runExport,
		},
		{
			name:  "import",
			short: "import Git AI or git-byline attribution notes",
			usage: "Usage: git-attributed import --format gitai|byline [--range REV-RANGE] [--dry-run]\n\n" +
				"Import Git AI notes from refs/notes/ai, or git-byline notes from\n" +
				"refs/notes/byline, into refs/notes/attributed. git-byline notes are\n" +
				"copied unchanged after they match their commit. A different existing\n" +
				"attributed note is never replaced. Without --range, only HEAD is\n" +
				"imported; --range HEAD imports its whole history.",
			run: runImport,
		},
		{
			name:  "install-hooks",
			short: "install agent and Git hooks",
			usage: "Usage: git-attributed install-hooks [--agent AGENTS] [--git] [--local-notes] [--user|--project] [--template]\n\n" +
				"Merge managed hooks without replacing existing configuration.\n" +
				"AGENTS is claude, droid, codex, copilot, or devin, a comma-separated\n" +
				"list of them, all, or none. The default is claude,droid.\n" +
				"Git hooks share attribution notes on push unless --local-notes is set.\n" +
				"--template manages Git hooks in the git-attributed Git template\n" +
				"directory, so every new git init and git clone is attributed.",
			run: runInstallHooks,
		},
		{
			name:  "uninstall",
			short: "remove managed hooks",
			usage: "Usage: git-attributed uninstall [--agent AGENTS] [--git] [--user|--project] [--template]\n\n" +
				"Remove only configuration managed by git-attributed. AGENTS takes the\n" +
				"same values as install-hooks; the default is claude,droid.",
			run: runUninstall,
		},
		{
			name:  "license",
			short: "show the license notices for this binary",
			usage: "Usage: git-attributed license\n\n" +
				"Print the MIT notices that cover this binary: Attributed's own and\n" +
				"the notice of git-byline, the upstream source it derives from.",
			run: runLicense,
		},
		{
			name:  "help",
			short: "show help for a command",
			usage: "Usage: git-attributed help [command]\n\n" +
				"Show general usage, or detailed usage for a single command.",
			run: runHelp,
		},
		{
			name:  "version",
			short: "show the git-attributed version",
			usage: "Usage: git-attributed version [--json]\n\n" +
				"Print the git-attributed version and exit. Release builds report the\n" +
				"release tag; local builds report dev. --json prints the version,\n" +
				"the highest supported attribution note version, and the state\n" +
				"format version.",
			run: runVersion,
		},
	}
}

// rootUsage returns the general usage text, derived from the command
// registry so the command list cannot drift from the registry itself.
func rootUsage() string {
	return buildRootUsage()
}

func buildRootUsage() string {
	var b strings.Builder
	b.WriteString("Usage: git-attributed <command> [flags]\n")
	b.WriteString("\n")
	b.WriteString("Track human and AI authorship line by line, from agent edits\n")
	b.WriteString("to commits. No cloud, no daemon, no background network.\n")
	b.WriteString("\n")
	b.WriteString("Commands:\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-14s %s\n", c.name, c.short)
	}
	b.WriteString("\nRun 'git-attributed help <command>' for details about a command.")
	return b.String()
}

func (env *Env) workingDir() (string, error) {
	if env.Dir != "" {
		return env.Dir, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read working directory: %w", err)
	}
	return dir, nil
}

func (env *Env) now() time.Time {
	if env.Now != nil {
		return env.Now()
	}
	return time.Now()
}

// lookup returns the registered command with the given name, or nil.
func lookup(name string) *command {
	for _, c := range commands() {
		if c.name == name {
			return c
		}
	}
	return nil
}

// Run dispatches args, the command line without the program name, and
// returns the process exit code together with the error that caused a
// non-zero code, if any. The caller decides how to exit the process.
func Run(args []string, env *Env) (int, error) {
	if env == nil {
		return ExitFailure, errors.New("app: env is nil")
	}
	if env.Stdin == nil || env.Stdout == nil || env.Stderr == nil {
		return ExitFailure, errors.New("app: env streams must not be nil")
	}
	if len(args) == 0 {
		return rootUsageError(env, errors.New("no command given"))
	}
	c := lookup(args[0])
	if c == nil {
		return rootUsageError(env, fmt.Errorf("unknown command %q", args[0]))
	}
	return c.run(env, c, args[1:])
}

// rootUsageError reports a dispatch-level usage problem on the error stream
// and returns the usage exit code.
func rootUsageError(env *Env, err error) (int, error) {
	fmt.Fprintf(env.Stderr, "git-attributed: %v\n\n%s\n", err, rootUsage())
	return ExitUsage, fmt.Errorf("git-attributed: usage: %w", err)
}

// commandUsageError reports a command-level usage problem on the error
// stream, followed by the command usage, and returns the usage exit code.
func commandUsageError(env *Env, c *command, err error) (int, error) {
	fmt.Fprintf(env.Stderr, "git-attributed %s: %v\n\n%s\n", c.name, err, c.usage)
	return ExitUsage, fmt.Errorf("git-attributed %s: usage: %w", c.name, err)
}

// runHelp implements the help command.
func runHelp(env *Env, c *command, args []string) (int, error) {
	switch len(args) {
	case 0:
		fmt.Fprintln(env.Stdout, rootUsage())
		return ExitSuccess, nil
	case 1:
		detail := lookup(args[0])
		if detail == nil {
			return rootUsageError(env, fmt.Errorf("unknown command %q", args[0]))
		}
		fmt.Fprintln(env.Stdout, detail.usage)
		return ExitSuccess, nil
	default:
		return commandUsageError(env, c, errors.New("help takes at most one command"))
	}
}

// runLicense implements the license command.
func runLicense(env *Env, c *command, args []string) (int, error) {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(env.Stdout, c.usage)
		return ExitSuccess, nil
	}
	if len(args) > 0 {
		return commandUsageError(env, c, fmt.Errorf("license takes no arguments, got %q", args[0]))
	}
	fmt.Fprint(env.Stdout, attributed.Notices())
	return ExitSuccess, nil
}

// versionJSON is the machine-readable form of the version command.
type versionJSON struct {
	Version      string `json:"version"`
	NoteVersion  int    `json:"note_version"`
	StateVersion int    `json:"state_version"`
}

// runVersion implements the version command. The default output is one
// stable line; --json adds the highest supported attribution note and
// state format versions for tooling that checks format compatibility
// before updating.
func runVersion(env *Env, c *command, args []string) (int, error) {
	fs := flag.NewFlagSet("git-attributed "+c.name, flag.ContinueOnError)
	var flagOutput strings.Builder
	fs.SetOutput(&flagOutput)
	fs.Usage = func() {
		fmt.Fprintln(&flagOutput, c.usage)
	}
	asJSON := fs.Bool("json", false, "print one machine-readable JSON object")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, c.usage)
			return ExitSuccess, nil
		}
		fmt.Fprint(env.Stderr, flagOutput.String())
		return ExitUsage, fmt.Errorf("git-attributed %s: %w", c.name, err)
	}
	if fs.NArg() > 0 {
		return commandUsageError(env, c, fmt.Errorf("version takes no arguments, got %q", fs.Arg(0)))
	}
	if *asJSON {
		data, err := json.Marshal(versionJSON{
			Version:      version.Version,
			NoteVersion:  model.NoteVersion,
			StateVersion: model.StateVersion,
		})
		if err != nil {
			return operationalError(env, c.name, fmt.Errorf("encode version JSON: %w", err))
		}
		fmt.Fprintf(env.Stdout, "%s\n", data)
		return ExitSuccess, nil
	}
	fmt.Fprintf(env.Stdout, "git-attributed %s\n", version.Version)
	return ExitSuccess, nil
}

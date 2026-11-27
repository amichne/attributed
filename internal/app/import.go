package app

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/amichne/attributed/internal/gitcmd"
	"github.com/amichne/attributed/internal/interop"
)

func runImport(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	format := flags.String("format", "", "gitai or byline")
	revisionRange := flags.String("range", "", "commit revision range")
	dryRun := flags.Bool("dry-run", false, "do not write notes")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("import takes no positional arguments"))
	}
	source, ok := importFormats[*format]
	if !ok {
		return commandUsageError(env, command, errors.New("--format must be gitai or byline"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := source.importer(repo, *revisionRange, *dryRun)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	writeWarnings(env, result.Warnings)
	if *dryRun {
		fmt.Fprintf(env.Stdout, "would import %d %s notes\n", result.Imported, source.label)
	} else {
		fmt.Fprintf(env.Stdout, "imported %d %s notes\n", result.Imported, source.label)
	}
	return ExitSuccess, nil
}

type noteImporter func(repo *gitcmd.Repo, revisionRange string, dryRun bool) (interop.ImportResult, error)

// importFormats maps each --format value to its importer and the source
// name printed in the summary.
var importFormats = map[string]struct {
	importer noteImporter
	label    string
}{
	"gitai":  {interop.ImportGitAI, "Git AI"},
	"byline": {interop.ImportByline, "git-byline"},
}

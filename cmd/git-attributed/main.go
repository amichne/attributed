// Command git-attributed is the git-attributed entry point.
//
// main only wires the process streams and exits; all behavior lives in
// internal/app so it can be tested without spawning processes.
package main

import (
	"os"

	"github.com/amichne/attributed/internal/app"
)

func main() {
	env := &app.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	code, err := app.Run(os.Args[1:], env)
	os.Exit(exitCode(code, err))
}

// exitCode maps a Run result to the process exit code. Run reports
// user-facing diagnostics through env streams. An error paired with a
// success code is a programming bug; treat it as an operational failure
// rather than exiting 0.
func exitCode(code int, err error) int {
	if err != nil && code == app.ExitSuccess {
		return app.ExitFailure
	}
	return code
}

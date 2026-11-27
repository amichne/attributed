# git-attributed plugin

Know which committed lines came from humans and AI without sending code,
prompts, or transcripts to a separate analytics service. Attribution notes
follow ordinary pushes to the selected Git remote by default.

This Factory plugin installs and configures git-attributed without a manual binary
installation step.

## Quick start

```text
droid plugin marketplace add amichne/attributed
droid plugin install git-attributed@git-attributed --scope user
```

Start Droid in a Git repository and run `/git-attributed-setup`. Then work
normally and inspect a committed file:

```sh
git attributed blame path/to/file
git attributed dashboard
git attributed status
```

## Capabilities

- `/git-attributed-setup` selects the operating-system installer.
- Release archives are verified against `checksums.txt`.
- Binary version is checked before installation.
- Local agent and Git hooks are activated without replacing unrelated config.
- The `git-attributed` skill documents status, blame, and local dashboard
  workflows.

The plugin setup installs a local runtime because Git hooks must keep working
outside an active Factory session. No daemon, account, telemetry, or cloud
storage is added. Git hook installation publishes attribution notes on
ordinary pushes by default. Use `--local-notes` to disable automatic note
sharing. Users can still push the notes ref explicitly.

See the [product overview](https://github.com/amichne/attributed),
[installation guide](https://github.com/amichne/attributed/blob/main/docs/INSTALL.md),
and [security model](https://github.com/amichne/attributed/blob/main/docs/SECURITY_MODEL.md).

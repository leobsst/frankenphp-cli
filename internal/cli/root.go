// Package cli wires the cobra commands. Commands stay thin: they parse the
// arguments and call internal/core, which the future web UI shares.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/ui"
	"github.com/leobsst/frankenphp-cli/internal/version"
)

// Exit codes of the CLI, kept stable for the scripts that rely on them.
const (
	ExitOK    = 0
	ExitError = 1 // business error, reported as "ERROR: <msg>"
	ExitUsage = 2 // missing or extra argument, invalid option or value
)

// usageError marks a command line the CLI cannot interpret (exit code 2),
// as opposed to a command that ran and failed (exit code 1).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// NewRootCmd builds the frankenmanager command, writing to out, with deps
// (nil fields: the real ones).
func NewRootCmd(out, errOut io.Writer, deps Deps) *cobra.Command {
	a := &app{deps: deps.withDefaults(), out: out, errOut: errOut, log: ui.NewLogger(out), con: ui.NewConsole(out)}
	root := &cobra.Command{
		Use:     "frankenmanager",
		Short:   "FrankenPHP Docker development environment manager",
		Version: version.Version,
		// Errors are printed by Execute, in its own format
		SilenceErrors: true,
		SilenceUsage:  true,
		// Arguments that match no subcommand reach the root: reject them
		// with a usage error, rather than letting cobra run the root.
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("No such command '%s'.", args[0])} //nolint:staticcheck // user-facing message
			}
			return nil
		},
		// The startup update notice, before every command but a few
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if cmd.HasParent() {
				a.notify(cmd, args)
			}
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return usageError{errors.New("Missing command.")} //nolint:staticcheck // user-facing message
		},
	}
	// No completion command
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(out)
	root.SetErr(errOut)
	// Same output as earlier releases, for --version and -v
	root.SetVersionTemplate("FrankenManager version {{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "Show version and exit")

	// Hidden global option used by the processes the CLI spawns itself. It
	// must precede the subcommand: `frankenmanager --no-update-check <cmd>`.
	root.PersistentFlags().Bool("no-update-check", false, "Skip checking for updates on startup")
	_ = root.PersistentFlags().MarkHidden("no-update-check")

	root.AddCommand(newStartCmd(a), newStopCmd(a), newRestartCmd(a),
		newAddHostCmd(a), newAddAliasCmd(a), newRemoveHostCmd(a), newRestoreHostCmd(a), newSwitchPHPCmd(a),
		newExecCmd(a), newListCmd(a), newStatusCmd(a), newTrustCACmd(a), newSetupCmd(a), newResetCmd(a), newUpdateCmd(a))

	root.AddCommand(newInternalCmds(a)...)

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err}
	})
	return root
}

// Execute runs the CLI with the real dependencies; see ExecuteWith.
func Execute(args []string, out, errOut io.Writer) int {
	return ExecuteWith(args, out, errOut, Deps{})
}

// ExecuteWith runs the CLI with args (without the program name) and returns the
// process exit code. Logs and errors go to out, and usage errors to errOut.
func ExecuteWith(args []string, out, errOut io.Writer, deps Deps) int {
	root := NewRootCmd(out, errOut, deps)
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		return ExitOK
	}

	var exit exitError
	if errors.As(err, &exit) {
		return int(exit)
	}

	var usage usageError
	if errors.As(err, &usage) {
		cmd, _, findErr := root.Find(args)
		if findErr != nil || cmd == nil {
			cmd = root
		}
		fmt.Fprintf(errOut, "Usage: %s\nTry '%s --help' for help.\n\nError: %s\n",
			cmd.UseLine(), cmd.CommandPath(), usage.err)
		return ExitUsage
	}

	ui.NewLogger(out).Error(err.Error())
	return ExitError
}

// Package cli builds the nfsense command tree. It is the only layer that
// knows about process exit codes, environment variables and flags; everything
// below it takes explicit arguments.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrodrig/nfsense/internal/service"
	"github.com/hrodrig/nfsense/internal/store"
)

// DefaultDataDir is where a packaged install keeps its state. It is only the
// last rung of the ladder (D-06/D-12): --data-dir, then NFSENSE_DATA_DIR.
const DefaultDataDir = "/var/lib/nfsense"

// EnvDataDir names the environment rung of the data-dir ladder.
const EnvDataDir = "NFSENSE_DATA_DIR"

// Exit codes are a contract with scripts (D-16).
const (
	exitOK       = 0
	exitGeneral  = 1
	exitUsage    = 2
	exitConflict = 3
	exitStoreBad = 4
)

// errUsage marks failures caused by how the command was invoked, so they map
// to exit code 2 rather than the catch-all 1.
var errUsage = errors.New("usage")

// usageError carries the operator-facing message while still matching
// errUsage via errors.Is.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func (e *usageError) Is(target error) bool { return target == errUsage }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// Run executes the command tree and returns the process exit code.
func Run(args []string) int {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "nfsense: %v\n", err)
		return exitCode(err)
	}
	return exitOK
}

// exitCode is the single place where a store error becomes a number.
func exitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, store.ErrConflict):
		return exitConflict
	case errors.Is(err, store.ErrSchemaTooNew),
		errors.Is(err, store.ErrCorruptDB),
		errors.Is(err, store.ErrNoDraft):
		return exitStoreBad
	case errors.Is(err, errUsage):
		return exitUsage
	default:
		return exitGeneral
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "nfsense",
		Short:         "Manage nftables intended state on a single Debian host",
		Long:          "nfsense keeps the intended firewall state in SQLite and applies it through an explicit draft, check, apply and rollback pipeline.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("data-dir", "", "state directory (default $"+EnvDataDir+" or "+DefaultDataDir+")")
	root.PersistentFlags().Bool("verbose", false, "print the resolved state directory and other diagnostics to stderr")
	root.AddCommand(newDraftCmd())
	return root
}

// resolveDataDir applies the ladder: flag, then environment, then the packaged
// default. There is no euid branch: running as root does not change the path.
func resolveDataDir(cmd *cobra.Command) (string, error) {
	dir, err := cmd.Flags().GetString("data-dir")
	if err != nil {
		return "", fmt.Errorf("read --data-dir: %w", err)
	}
	if dir != "" {
		return dir, nil
	}
	if env := os.Getenv(EnvDataDir); env != "" {
		return env, nil
	}
	return DefaultDataDir, nil
}

// withService resolves the data directory, opens the service, and always
// closes it before returning the callback's error.
func withService(cmd *cobra.Command, fn func(context.Context, *service.Service) error) error {
	dir, err := resolveDataDir(cmd)
	if err != nil {
		return err
	}
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("read --verbose: %w", err)
	}
	svc, err := service.Open(cmd.Context(), dir)
	if err != nil {
		return err
	}
	defer func() { _ = svc.Close() }()
	if verbose {
		fmt.Fprintf(cmd.ErrOrStderr(), "data-dir=%s\n", dir)
	}
	return fn(cmd.Context(), svc)
}

// localActor attributes a CLI invocation to the human behind it. Under sudo
// that is SUDO_USER, not root: the person typed the command (D-13).
func localActor() service.Actor {
	return service.Actor{Kind: service.KindLocal, Name: localName()}
}

func localName() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	return "root"
}

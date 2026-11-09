package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrodrig/nfsense/internal/draftdoc"
	"github.com/hrodrig/nfsense/internal/service"
	"github.com/hrodrig/nfsense/internal/store"
)

func newDraftCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "draft",
		Short: "Work with the single intended-state draft",
	}
	cmd.AddCommand(newDraftShowCmd(), newDraftSetCmd())
	return cmd
}

func newDraftShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the working draft document",
		Long: "Print the working draft document. Without --json the stored bytes are\n" +
			"written verbatim, so the output is byte-identical to what was written.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withService(cmd, func(ctx context.Context, svc *service.Service) error {
				d, err := svc.GetDraft(ctx, localActor())
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				if asJSON {
					if err := writeDraftJSON(out, d); err != nil {
						return err
					}
				} else if _, err := out.Write(d.Document); err != nil {
					return fmt.Errorf("write draft: %w", err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "revision=%d\n", d.Revision)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the envelope with the current revision merged in")
	return cmd
}

// writeDraftJSON merges revision into the envelope without re-encoding the
// body, so even --json output keeps unknown keys intact.
func writeDraftJSON(w io.Writer, d store.Draft) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(d.Document, &fields); err != nil {
		return fmt.Errorf("decode stored draft: %w", err)
	}
	revision, err := json.Marshal(d.Revision)
	if err != nil {
		return fmt.Errorf("encode revision: %w", err)
	}
	fields["revision"] = revision
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(fields); err != nil {
		return fmt.Errorf("write draft json: %w", err)
	}
	return nil
}

func newDraftSetCmd() *cobra.Command {
	var (
		file       string
		ifRevision int64
		force      bool
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Replace the working draft document",
		Long: "Replace the working draft document from a file ('-' for stdin).\n\n" +
			"The write is a compare-and-swap: --if-revision states the revision you\n" +
			"read, and the write fails with exit code 3 if the draft moved on.\n" +
			"--force overwrites unconditionally and exists for recovery only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return usagef("draft set requires --file <path> (- for stdin)")
			}
			if cmd.Flags().Changed("if-revision") == force {
				return usagef("draft set requires exactly one of --if-revision or --force")
			}
			raw, err := readDocument(cmd, file)
			if err != nil {
				return err
			}
			return withService(cmd, func(ctx context.Context, svc *service.Service) error {
				actor := localActor()
				expected := ifRevision
				if force {
					// --force still writes through the compare-and-swap; it
					// just spares the operator from naming the revision they
					// read. Plan 04 is where recovery semantics land.
					d, err := svc.GetDraft(ctx, actor)
					if err != nil {
						return err
					}
					expected = d.Revision
				}
				rev, err := svc.SaveDraft(ctx, actor, raw, expected)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "revision=%d\n", rev)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the document from <path>, or - for stdin")
	cmd.Flags().Int64Var(&ifRevision, "if-revision", 0, "expected current revision for a compare-and-swap write")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite without a revision check (recovery only)")
	return cmd
}

// readDocument reads at most MaxBytes+1 so an oversized stream is rejected
// without being buffered whole.
func readDocument(cmd *cobra.Command, file string) ([]byte, error) {
	var (
		r   io.Reader
		err error
	)
	if file == "-" {
		r = cmd.InOrStdin()
	} else {
		f, openErr := os.Open(file)
		if openErr != nil {
			return nil, fmt.Errorf("read %s: %w", file, openErr)
		}
		defer f.Close()
		r = f
	}
	raw, err := io.ReadAll(io.LimitReader(r, draftdoc.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	if len(raw) > draftdoc.MaxBytes {
		return nil, fmt.Errorf("document at %s exceeds the %d byte limit", file, draftdoc.MaxBytes)
	}
	return raw, nil
}

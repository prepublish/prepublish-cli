package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

func (a *app) reportCmd() *cobra.Command {
	var (
		open  bool
		pager bool
	)
	cmd := &cobra.Command{
		Use:   "report ID",
		Short: "Print a stored report",
		Long: "Print one audit, by id or by its report URL.\n\n" +
			"A report link is public: the id works as the share key, so this needs no\n" +
			"credential. What it shows still follows the owner's entitlement — a gated\n" +
			"report stays gated.",
		Example: "  prepublish report 6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f\n" +
			"  prepublish report https://prepublish.ai/analysis/6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f\n" +
			"  prepublish report 6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f --open",
		Args: a.args(func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("a report id is required: `prepublish report <id>`")
			}
			return nil
		}),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runReport(ctxOf(cmd), cmd, args[0], open, pager)
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "open the report in a browser")
	cmd.Flags().BoolVar(&pager, "pager", false, "always open the report in the pager")
	return cmd
}

func (a *app) runReport(ctx context.Context, cmd *cobra.Command, ref string, open, pager bool) error {
	id, err := reportRef(ref)
	if err != nil {
		return usageErr(err)
	}

	analysis, err := a.client.GetAnalysis(ctx, id)
	if err != nil {
		return err
	}
	if open {
		a.openBrowser(ui.ReportURL(a.cfg.AppURL(), analysis.ID))
	}
	return a.showReport(analysis, pager)
}

// reportRef turns what someone pasted into a report id. Both the id and the
// report URL are accepted, because the URL is what is on the clipboard after
// reading a report in the browser or in this CLI's own footer.
//
// The id is checked here rather than at the API: a typo deserves "that is not a
// report id" in a millisecond, not a round trip that answers
// "invalid analysis ID".
func reportRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("a report id is required: `prepublish report <id>`")
	}

	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "/analysis/") {
		parsed, err := url.Parse(ref)
		if err != nil {
			return "", fmt.Errorf("%s is neither a report id nor a report URL", ref)
		}
		path := strings.TrimRight(parsed.Path, "/")
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			ref = path[idx+1:]
		} else {
			ref = path
		}
	}

	if !isUUID(ref) {
		return "", fmt.Errorf("that is not a report id: pass the id or its URL (%s/analysis/<id>)",
			config.DefaultAppURL)
	}
	return ref, nil
}

// isUUID accepts the canonical 8-4-4-4-12 hexadecimal form, in either case.
// The API's analysis ids are UUIDs, and the check is deliberately about shape
// rather than about a specific version.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

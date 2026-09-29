package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/tui"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

// historyPerPage is the page size for the plain listing and the first page the
// interactive browser loads. Ten rows fit a 24-line terminal with the chrome.
const historyPerPage = 10

func (a *app) historyCmd() *cobra.Command {
	var page int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List your past audits",
		Long: "List the audits on your account, newest first.\n\n" +
			"In a terminal this is a browsable list: enter opens a report in the pager,\n" +
			"esc goes back. Piped, it prints one line per audit so it can be filtered.",
		Example: "  prepublish history\n" +
			"  prepublish history --json | jq '.analyses[].overall_score'",
		Args: a.args(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runHistory(ctxOf(cmd), page)
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "page number")
	return cmd
}

func (a *app) runHistory(ctx context.Context, page int) error {
	if !a.signedIn() {
		return exitErr(ExitAuth, errors.New("history needs an account: run `prepublish login`"))
	}
	if page < 1 {
		page = 1
	}

	fetch := func(ctx context.Context, page int) (*api.AnalysisPage, error) {
		if page < 1 {
			page = 1
		}
		return a.client.ListAnalyses(ctx, page, historyPerPage)
	}

	if a.interactive() {
		open := func(ctx context.Context, id string) (string, error) {
			analysis, err := a.client.GetAnalysis(ctx, id)
			if err != nil {
				return "", err
			}
			return ui.RenderReport(analysis, a.width), nil
		}
		return tui.RunHistory(ctx, fetch, open)
	}

	page1, err := fetch(ctx, page)
	if err != nil {
		return err
	}
	if a.jsonMode {
		return a.printJSON(page1)
	}

	if len(page1.Analyses) == 0 {
		a.say(ui.Info("No audits yet: run `prepublish audit script.md --title \"...\"`"))
		return nil
	}

	out := &strings.Builder{}
	table := tabwriter.NewWriter(out, 0, 4, 1, ' ', 0)
	fmt.Fprintln(table, "ID\tDATE\tSCORE\tTITLE")
	for _, analysis := range page1.Analyses {
		date := "—"
		if !analysis.CreatedAt.IsZero() {
			date = analysis.CreatedAt.Local().Format("2006-01-02")
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n",
			analysis.ID, date, score(analysis), oneLine(analysis.VideoTitle, a.titleColumn()))
	}
	_ = table.Flush()

	a.result(strings.TrimRight(out.String(), "\n"))
	if page1.Total > len(page1.Analyses) {
		a.say(ui.Info(fmt.Sprintf("page %d · %s", page1.Page, count(page1.Total, "audit"))))
	}
	return nil
}

// titleColumn is the room left for a title after the id, date and score
// columns.
//
// The full id is what makes a row usable: it is the value `prepublish report`
// takes, and a truncated one cannot be copied anywhere. So the title — the only
// column a reader can reconstruct from the id — gives way first on a narrow
// terminal.
func (a *app) titleColumn() int {
	const fixed = 36 + 1 + 10 + 1 + 9 + 1 // uuid, date, score, and the gaps
	return max(a.width-fixed, 16)
}

// score renders the overall score, or the status for an audit that has not
// finished.
func score(analysis *api.Analysis) string {
	if analysis.Status != api.StatusCompleted && analysis.Status != api.StatusFailed {
		return analysis.Status
	}
	return fmt.Sprintf("%d", analysis.OverallScore)
}

// oneLine keeps a timeline readable: titles are one line each, and a long one is
// cut to the column rather than wrapped into the next row.
func oneLine(title string, limit int) string {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "(untitled)"
	}
	if runes := []rune(title); len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return title
}

// count pluralizes a small count for a footer line.
func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

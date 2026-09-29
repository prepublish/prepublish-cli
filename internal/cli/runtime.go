package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prepublish/prepublish-cli/internal/scriptinfo"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

func (a *app) runtimeCmd() *cobra.Command {
	var minutes int
	cmd := &cobra.Command{
		Use:   "runtime [FILE|-]",
		Short: "Word count and speaking time, without the network",
		Long: "Count a script's words and how long they take to say, at a measured, a\n" +
			"typical and an energetic pace.\n\n" +
			"This is local arithmetic: no account, no API call, no waiting. Use it to\n" +
			"check whether a draft fits the slot before paying for an audit, or with\n" +
			"--minutes to see how many words a video of that length needs.",
		Example: "  prepublish runtime script.md\n" +
			"  cat script.md | prepublish runtime -\n" +
			"  prepublish runtime --minutes 10",
		Args: a.args(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			return a.runRuntime(cmd, path, minutes)
		},
	}
	cmd.Flags().IntVar(&minutes, "minutes", 0,
		"report the word counts a video of this many minutes needs, instead of reading a script")
	return cmd
}

func (a *app) runRuntime(cmd *cobra.Command, path string, minutes int) error {
	if minutes > 0 {
		if strings.TrimSpace(path) != "" {
			return usageErr(errors.New("pass a script or --minutes, not both"))
		}
		slow, typical, fast := scriptinfo.WordsFor(time.Duration(minutes) * time.Minute)
		if a.jsonMode {
			return a.printJSON(struct {
				Minutes      int `json:"minutes"`
				WordsSlow    int `json:"words_slow"`
				WordsTypical int `json:"words_typical"`
				WordsFast    int `json:"words_fast"`
			}{Minutes: minutes, WordsSlow: slow, WordsTypical: typical, WordsFast: fast})
		}
		a.result(ui.Info(fmt.Sprintf("A %d-minute video needs about:", minutes)))
		a.result(fmt.Sprintf("  measured   %5d words", slow))
		a.result(fmt.Sprintf("  typical    %5d words", typical))
		a.result(fmt.Sprintf("  energetic  %5d words", fast))
		return nil
	}

	path, err := a.scriptPath(cmd, path)
	if err != nil {
		return err
	}
	if isMediaFile(path) {
		return usageErr(fmt.Errorf(
			"%s is a video or audio file: this counts the words in a script (audit a recording to get its transcript)",
			path))
	}

	text, err := readScript(path, os.Stdin)
	if err != nil {
		return err
	}
	if len(text) == 0 {
		return usageErr(fmt.Errorf("%s is empty", describe(path)))
	}

	words := scriptinfo.WordCount(text)
	estimate := scriptinfo.Runtime(words)

	if a.jsonMode {
		// The seconds are truncated to match what the card shows. A script and
		// a person looking at the same estimate should read the same number,
		// and the estimate is a range anyway.
		return a.printJSON(struct {
			Words   int    `json:"words"`
			Slow    string `json:"slow"`
			Typical string `json:"typical"`
			Fast    string `json:"fast"`
		}{
			Words:   words,
			Slow:    estimate.Slow.Truncate(time.Second).String(),
			Typical: estimate.Typical.Truncate(time.Second).String(),
			Fast:    estimate.Fast.Truncate(time.Second).String(),
		})
	}

	a.result(ui.RenderRuntime(words, estimate, a.width))
	return nil
}

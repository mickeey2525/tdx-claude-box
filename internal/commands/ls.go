package commands

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/mickeey2525/tdx-claude-box/internal/config"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// Ls は tcb 管理の box 一覧を表示する。
func Ls(e engine.Engine, w io.Writer) error {
	boxes, err := e.ListBoxes(config.LabelSite, config.LabelWorkdir)
	if err != nil {
		return err
	}
	if len(boxes) == 0 {
		fmt.Fprintln(w, "no boxes (create one with 'tcb run <site>')")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SITE\tSTATE\tWORKDIR\tUP\tCONTAINER")
	for _, b := range boxes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", b.Site, b.State, b.Workdir, b.RunningFor, b.Name)
	}
	return tw.Flush()
}

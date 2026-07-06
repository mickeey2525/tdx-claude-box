package commands

import (
	"fmt"

	"github.com/mickeey2525/tdx-claude-box/internal/config"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
	"github.com/mickeey2525/tdx-claude-box/internal/site"
)

// Shell は box に bash で入る(デバッグ・tdx auth setup のやり直し用)。
func Shell(e engine.Engine, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: tcb shell <site>")
	}
	siteName := args[0]
	if err := site.Validate(siteName); err != nil {
		return err
	}
	name := config.ContainerName(siteName)
	state, err := e.ContainerState(name)
	if err != nil {
		return err
	}
	if state == "" {
		return fmt.Errorf("no box for site %q (create one with 'tcb run %s')", siteName, siteName)
	}
	if state != "running" {
		if err := e.Start(name); err != nil {
			return err
		}
	}
	opts := sessionExecOpts(name, []string{"bash", "-l"})
	if b, addr := startSessionBridge(e, name); b != nil {
		defer b.Close()
		opts.Env["TCB_BRIDGE"] = addr
	}
	return e.ExecInteractive(opts)
}

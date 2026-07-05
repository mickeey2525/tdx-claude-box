package commands

import (
	"fmt"

	"github.com/mickeey2525/tdx-claude-box/internal/config"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
	"github.com/mickeey2525/tdx-claude-box/internal/site"
)

// Stop は box のコンテナを停止する(HOME ボリュームは残る)。
func Stop(e engine.Engine, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: tcb stop <site>")
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
	switch state {
	case "":
		return fmt.Errorf("no box for site %q", siteName)
	case "running":
		if err := e.Stop(name); err != nil {
			return err
		}
		fmt.Printf("stopped %s\n", name)
	default:
		fmt.Printf("%s is already %s\n", name, state)
	}
	return nil
}

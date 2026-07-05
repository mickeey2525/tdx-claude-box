package commands

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/mickeey2525/tdx-claude-box/internal/config"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
	"github.com/mickeey2525/tdx-claude-box/internal/site"
)

// Rm は box のコンテナを削除する。--volumes 指定時は HOME ボリューム
// (tdx の認証情報・~/.claude を含む)も確認プロンプトの上で削除する。
func Rm(e engine.Engine, args []string, stdin io.Reader, stdout io.Writer) error {
	var siteName string
	var withVolumes bool
	for _, a := range args {
		switch {
		case a == "--volumes":
			withVolumes = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q for rm", a)
		case siteName == "":
			siteName = a
		default:
			return fmt.Errorf("unexpected argument %q", a)
		}
	}
	if siteName == "" {
		return fmt.Errorf("usage: tcb rm <site> [--volumes]")
	}
	if err := site.Validate(siteName); err != nil {
		return err
	}

	name := config.ContainerName(siteName)
	volume := config.VolumeName(siteName)

	state, err := e.ContainerState(name)
	if err != nil {
		return err
	}
	_, volumeExists, err := e.VolumeSiteLabel(volume, config.LabelSite)
	if err != nil {
		return err
	}
	if state == "" && !volumeExists {
		return fmt.Errorf("no box for site %q", siteName)
	}

	if withVolumes && volumeExists {
		fmt.Fprintf(stdout, "This will delete volume %s including tdx credentials and ~/.claude for site %q.\n", volume, siteName)
		fmt.Fprintf(stdout, "Type the site name to confirm: ")
		scanner := bufio.NewScanner(stdin)
		if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != siteName {
			return fmt.Errorf("aborted")
		}
	}

	if state != "" {
		if err := e.Remove(name); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed container %s\n", name)
	}
	if withVolumes && volumeExists {
		if err := e.VolumeRemove(volume); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed volume %s\n", volume)
	} else if volumeExists {
		fmt.Fprintf(stdout, "kept volume %s (use --volumes to delete credentials too)\n", volume)
	}
	return nil
}

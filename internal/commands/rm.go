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

// Rm は box を削除する。既定でコンテナと HOME ボリューム
// (tdx の API キー・~/.claude を含む)の両方を消す。ボリューム削除の前には
// 警告と確認プロンプトを出す。--keep-volume でボリュームだけ残せる。
func Rm(e engine.Engine, args []string, stdin io.Reader, stdout io.Writer) error {
	var siteName string
	var keepVolume, force bool
	for _, a := range args {
		switch {
		case a == "--keep-volume":
			keepVolume = true
		case a == "--volumes":
			// 旧フラグ。ボリューム削除は現在の既定動作なので受け付けるだけ
		case a == "-f" || a == "--force":
			force = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q for rm", a)
		case siteName == "":
			siteName = a
		default:
			return fmt.Errorf("unexpected argument %q", a)
		}
	}
	if siteName == "" {
		return fmt.Errorf("usage: tcb rm <box> [--keep-volume] [--force]")
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

	deleteVolume := volumeExists && !keepVolume
	if deleteVolume && !force {
		fmt.Fprintf(stdout, "WARNING: volume %s will be deleted too.\n", volume)
		fmt.Fprintf(stdout, "It holds the tdx API key and ~/.claude for box %q.\n", siteName)
		fmt.Fprintf(stdout, "(use --keep-volume to keep credentials) Continue? [y/N]: ")
		scanner := bufio.NewScanner(stdin)
		answer := ""
		if scanner.Scan() {
			answer = strings.TrimSpace(scanner.Text())
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			return fmt.Errorf("aborted")
		}
	}

	if state != "" {
		if err := e.Remove(name); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed container %s\n", name)
	}
	if deleteVolume {
		if err := e.VolumeRemove(volume); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed volume %s\n", volume)
	} else if volumeExists {
		fmt.Fprintf(stdout, "kept volume %s (credentials preserved; reused by the next 'tcb run %s')\n", volume, siteName)
	}
	return nil
}

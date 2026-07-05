// Package commands は tcb のサブコマンド実装。
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mickeey2525/tdx-claude-box/image"
	"github.com/mickeey2525/tdx-claude-box/internal/config"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
	"github.com/mickeey2525/tdx-claude-box/internal/site"
)

type runOptions struct {
	Site string
	// TDSite は tdx use site に渡す実際の TD site。空なら Site と同じ。
	// box 名と分けることで「同じ TD site の別アカウント用 box」を作れる
	// (例: tcb run us01-7060 --site us01)。
	TDSite      string
	Dir         string
	Rebuild     bool
	Passthrough []string
}

// parseRunArgs は `tcb run` の引数を解釈する。`--` 以降は tdx claude へ渡す。
func parseRunArgs(args []string) (runOptions, error) {
	var o runOptions
	rest := args
	for i, a := range args {
		if a == "--" {
			o.Passthrough = args[i+1:]
			rest = args[:i]
			break
		}
	}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--dir":
			i++
			if i >= len(rest) {
				return o, fmt.Errorf("--dir requires a path")
			}
			o.Dir = rest[i]
		case strings.HasPrefix(a, "--dir="):
			o.Dir = strings.TrimPrefix(a, "--dir=")
		case a == "--site":
			i++
			if i >= len(rest) {
				return o, fmt.Errorf("--site requires a TD site name")
			}
			o.TDSite = rest[i]
		case strings.HasPrefix(a, "--site="):
			o.TDSite = strings.TrimPrefix(a, "--site=")
		case a == "--rebuild":
			o.Rebuild = true
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %q for run", a)
		case o.Site == "":
			o.Site = a
		default:
			return o, fmt.Errorf("unexpected argument %q (pass tdx claude args after --)", a)
		}
	}
	if o.Site == "" {
		return o, fmt.Errorf("usage: tcb run <box> [--site <td-site>] [--dir <path>] [--rebuild] [-- <tdx claude args...>]")
	}
	return o, nil
}

// Run は site 用コンテナを起動(なければ作成)して tdx claude セッションに入る。
func Run(e engine.Engine, args []string) error {
	o, err := parseRunArgs(args)
	if err != nil {
		return err
	}
	if err := site.Validate(o.Site); err != nil {
		return err
	}
	explicitTDSite := o.TDSite != ""
	if o.TDSite == "" {
		o.TDSite = o.Site
	}
	if err := site.Validate(o.TDSite); err != nil {
		return fmt.Errorf("--site: %w", err)
	}

	if o.Rebuild || !e.ImageExists(config.ImageTag) {
		if err := buildImage(e, o.Rebuild); err != nil {
			return err
		}
	}

	workdir, explicitDir, err := resolveWorkdir(o)
	if err != nil {
		return err
	}

	if err := ensureVolume(e, o.Site); err != nil {
		return err
	}

	name := config.ContainerName(o.Site)
	state, err := e.ContainerState(name)
	if err != nil {
		return err
	}
	switch state {
	case "":
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			return fmt.Errorf("create workdir: %w", err)
		}
		fmt.Fprintf(os.Stderr, "tcb: creating box %s (workdir: %s)\n", name, workdir)
		err = e.RunDetached(engine.RunOpts{
			Name:     name,
			Hostname: config.Hostname(o.Site),
			Image:    config.ImageTag,
			Labels: map[string]string{
				config.LabelSite:    o.Site,
				config.LabelWorkdir: workdir,
				config.LabelTDSite:  o.TDSite,
			},
			Env: map[string]string{
				"TCB_SITE":    o.Site,
				"TCB_TD_SITE": o.TDSite,
			},
			Volumes: []string{
				config.VolumeName(o.Site) + ":" + config.HomeMount,
				workdir + ":" + config.WorkMount,
			},
			Workdir: config.WorkMount,
			Command: []string{config.BootCommand},
		})
		if err != nil {
			return err
		}
	default:
		// 既存コンテナは作成時の workdir マウント・TD site 設定のまま動く
		if err := checkExistingWorkdir(e, name, workdir, explicitDir, o.Site); err != nil {
			return err
		}
		if err := checkExistingTDSite(e, name, o.TDSite, explicitTDSite, o.Site); err != nil {
			return err
		}
		if state != "running" {
			if err := e.Start(name); err != nil {
				return err
			}
		}
	}

	command := append([]string{config.EntryCommand}, o.Passthrough...)
	return e.ExecInteractive(sessionExecOpts(name, command))
}

// sessionExecOpts は box 内セッション実行の共通オプションを返す。
func sessionExecOpts(name string, command []string) engine.ExecOpts {
	return engine.ExecOpts{
		Name:    name,
		Workdir: config.WorkMount,
		User:    config.SessionUser,
		// Apple container の exec は --user を指定しても HOME を設定しないため明示する
		Env:     map[string]string{"HOME": config.HomeMount},
		Command: command,
	}
}

// resolveWorkdir は作業ディレクトリを絶対パスで返す。
func resolveWorkdir(o runOptions) (dir string, explicit bool, err error) {
	if o.Dir != "" {
		abs, err := filepath.Abs(o.Dir)
		if err != nil {
			return "", false, fmt.Errorf("resolve --dir: %w", err)
		}
		return abs, true, nil
	}
	dir, err = config.DefaultWorkdir(o.Site)
	return dir, false, err
}

// ensureVolume は site 用 HOME ボリュームを検証つきで用意する。
// 別 site のボリュームを誤って使うことを防ぐ第一のガードレール
// (第二はコンテナ内 entrypoint のマーカーファイル検証)。
func ensureVolume(e engine.Engine, siteName string) error {
	volume := config.VolumeName(siteName)
	label, exists, err := e.VolumeSiteLabel(volume, config.LabelSite)
	if err != nil {
		return err
	}
	if !exists {
		return e.VolumeCreate(volume, map[string]string{config.LabelSite: siteName})
	}
	if label != siteName {
		return fmt.Errorf("volume %s exists but is not labeled for site %q (label: %q); remove it manually if it is stale", volume, siteName, label)
	}
	return nil
}

// checkExistingTDSite は --site 指定が既存コンテナの設定と食い違っていないか確認する。
// TD site はコンテナ作成時に環境変数で焼き込まれるため、変えるには作り直しが必要。
func checkExistingTDSite(e engine.Engine, name, tdSite string, explicit bool, boxName string) error {
	if !explicit {
		return nil
	}
	current, err := e.ContainerLabel(name, config.LabelTDSite)
	if err != nil {
		return err
	}
	if current == "" {
		current = boxName // ラベルがない旧コンテナは box 名 = TD site
	}
	if current != tdSite {
		return fmt.Errorf("box %s already uses TD site %q; run 'tcb rm %s' first to switch to %q", name, current, boxName, tdSite)
	}
	return nil
}

// checkExistingWorkdir は --dir 指定が既存コンテナのマウントと食い違っていないか確認する。
func checkExistingWorkdir(e engine.Engine, name, workdir string, explicit bool, siteName string) error {
	if !explicit {
		return nil
	}
	current, err := e.ContainerLabel(name, config.LabelWorkdir)
	if err != nil {
		return err
	}
	if current != workdir {
		return fmt.Errorf("box %s already uses workdir %s; run 'tcb rm %s' first to switch to %s", name, current, siteName, workdir)
	}
	return nil
}

// buildImage は埋め込みビルドコンテキストからイメージをビルドする。
// コンテキストはユーザーキャッシュ配下に置く。macOS の TMPDIR(/var/folders)は
// Apple container のビルダーから読めないため使わない。
// rebuild 時は --no-cache でビルドし、@latest のパッケージを実際に更新する。
func buildImage(e engine.Engine, rebuild bool) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("resolve cache directory: %w", err)
	}
	dir := filepath.Join(cache, "tcb", "build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}
	if err := image.WriteBuildContext(dir); err != nil {
		return err
	}
	opts := engine.BuildOpts{NoCache: rebuild, BuildArgs: map[string]string{}}
	if v := os.Getenv("TCB_TDX_VERSION"); v != "" {
		opts.BuildArgs["TDX_VERSION"] = v
	}
	if v := os.Getenv("TCB_CLAUDE_CODE_VERSION"); v != "" {
		opts.BuildArgs["CLAUDE_CODE_VERSION"] = v
	}
	fmt.Fprintf(os.Stderr, "tcb: building image %s\n", config.ImageTag)
	return e.Build(dir, config.ImageTag, opts)
}

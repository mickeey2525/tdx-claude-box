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

var projectSettingsFiles = []string{
	filepath.Join(".claude", "settings.json"),
}

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
		// box 名 "us01-7060" のような形式なら TD site "us01" を自動導出する
		o.TDSite = site.DeriveTDSite(o.Site)
	}
	if err := site.Validate(o.TDSite); err != nil {
		return fmt.Errorf("--site: %w", err)
	}
	if o.TDSite != o.Site {
		fmt.Fprintf(os.Stderr, "tcb: box %q uses TD site %q\n", o.Site, o.TDSite)
	}

	if err := ensureImage(e, o.Rebuild); err != nil {
		return err
	}

	workdir, explicitDir, err := resolveWorkdir(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return fmt.Errorf("create workdir: %w", err)
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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	if err := syncProjectSettings(cwd, workdir); err != nil {
		return fmt.Errorf("sync project settings: %w", err)
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

// syncProjectSettings は tcb を実行したプロジェクトの共有設定だけを box 側 workdir へ反映する。
// tdx claude が site ごとに書く settings.local.json は同期しない。
func syncProjectSettings(srcDir, workdir string) error {
	srcAbs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("resolve source directory: %w", err)
	}
	dstAbs, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}
	if filepath.Clean(srcAbs) == filepath.Clean(dstAbs) {
		return nil
	}

	for _, rel := range projectSettingsFiles {
		if err := syncProjectSettingsFile(srcAbs, dstAbs, rel); err != nil {
			return err
		}
	}
	return nil
}

func syncProjectSettingsFile(srcDir, workdir, rel string) error {
	src := filepath.Join(srcDir, rel)
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return removeManagedProjectSettings(workdir, rel)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	dst := filepath.Join(workdir, rel)
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(projectSettingsMarker(dst), []byte(src+"\n"), 0o644)
}

func removeManagedProjectSettings(workdir, rel string) error {
	dst := filepath.Join(workdir, rel)
	marker := projectSettingsMarker(dst)
	if _, err := os.Stat(marker); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func projectSettingsMarker(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), ".tcb-settings-source")
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
	switch label {
	case siteName:
		return nil
	case "":
		// ラベルは後付けできないので、ラベルなしボリュームは警告して採用する。
		// site の取り違えはコンテナ内のマーカー検証が防ぐ。
		fmt.Fprintf(os.Stderr, "tcb: warning: adopting existing volume %s without a %s label (site is still verified by the in-volume marker)\n", volume, config.LabelSite)
		return nil
	default:
		return fmt.Errorf("volume %s belongs to site %q, not %q; remove it manually if it is stale", volume, label, siteName)
	}
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
		current = site.DeriveTDSite(boxName) // ラベルがない旧コンテナは box 名から導出
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

// ensureImage は tcb:latest を用意する。
//
// カスタム Dockerfile なし: rebuild またはイメージ未存在のときだけ
// 埋め込みコンテキストからビルドする。
// カスタム Dockerfile あり(~/.config/tcb/Dockerfile または TCB_DOCKERFILE):
// 埋め込みイメージを tcb:base として用意し、その上にカスタム層を重ねた結果を
// tcb:latest にする。カスタム層は毎回ビルドする(キャッシュが効くので
// 変更がなければ一瞬。Dockerfile の変更が --rebuild なしで反映される)。
//
// rebuild 時は --no-cache でビルドし、@latest のパッケージを実際に更新する。
func ensureImage(e engine.Engine, rebuild bool) error {
	custom, err := customDockerfile()
	if err != nil {
		return err
	}

	if custom == "" {
		if !rebuild && e.ImageExists(config.ImageTag) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "tcb: building image %s\n", config.ImageTag)
		return buildEmbedded(e, config.ImageTag, rebuild)
	}

	if rebuild || !e.ImageExists(config.BaseImageTag) {
		fmt.Fprintf(os.Stderr, "tcb: building base image %s\n", config.BaseImageTag)
		if err := buildEmbedded(e, config.BaseImageTag, rebuild); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "tcb: building %s from custom Dockerfile %s\n", config.ImageTag, custom)
	return e.Build(filepath.Dir(custom), config.ImageTag, engine.BuildOpts{
		NoCache:    rebuild,
		Dockerfile: custom,
	})
}

// buildEmbedded は埋め込みビルドコンテキストからイメージをビルドする。
// コンテキストはユーザーキャッシュ配下に置く。macOS の TMPDIR(/var/folders)は
// Apple container のビルダーから読めないため使わない。
func buildEmbedded(e engine.Engine, tag string, noCache bool) error {
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
	opts := engine.BuildOpts{NoCache: noCache, BuildArgs: map[string]string{}}
	if v := os.Getenv("TCB_TDX_VERSION"); v != "" {
		opts.BuildArgs["TDX_VERSION"] = v
	}
	if v := os.Getenv("TCB_CLAUDE_CODE_VERSION"); v != "" {
		opts.BuildArgs["CLAUDE_CODE_VERSION"] = v
	}
	return e.Build(dir, tag, opts)
}

// customDockerfile はユーザーのカスタム Dockerfile のパスを返す(なければ空)。
// TCB_DOCKERFILE 環境変数が最優先("none" で明示的に無効化)、
// 次に ~/.config/tcb/Dockerfile が存在すれば使う。
func customDockerfile() (string, error) {
	if v, ok := os.LookupEnv("TCB_DOCKERFILE"); ok {
		if v == "" || v == "none" {
			return "", nil
		}
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", fmt.Errorf("TCB_DOCKERFILE: %w", err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("TCB_DOCKERFILE: %w", err)
		}
		return abs, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}
	p := filepath.Join(home, ".config", "tcb", "Dockerfile")
	if _, err := os.Stat(p); err != nil {
		return "", nil
	}
	return p, nil
}

// Package engine はコンテナランタイムの抽象化を提供する。
// Docker と Apple container の2バックエンドを実装し、tcb のコマンド層は
// Engine インターフェースだけに依存する。
package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Engine は tcb が必要とするコンテナランタイム操作。
type Engine interface {
	// Name はバックエンド名("docker" / "apple")。
	Name() string
	// Available はランタイムが使用可能か確認する。
	Available() error

	ImageExists(tag string) bool
	// Build は ctxDir をビルドコンテキストにイメージをビルドする(出力は端末へ)。
	Build(ctxDir, tag string, o BuildOpts) error

	// ContainerState はコンテナの状態("running", "stopped"/"exited" など)を返す。
	// 存在しない場合は空文字列。
	ContainerState(name string) (string, error)
	// ContainerLabel はコンテナのラベル値を返す。
	ContainerLabel(name, key string) (string, error)
	RunDetached(o RunOpts) error
	Start(name string) error
	// ExecInteractive はコンテナ内でコマンドを対話実行する(TTY 前提)。
	ExecInteractive(o ExecOpts) error
	Stop(name string) error
	// Remove はコンテナを(実行中でも)削除する。
	Remove(name string) error

	// VolumeSiteLabel はボリュームのラベル値と存在有無を返す。
	VolumeSiteLabel(name, key string) (value string, exists bool, err error)
	VolumeCreate(name string, labels map[string]string) error
	VolumeRemove(name string) error

	// ListBoxes は siteLabel を持つコンテナを列挙する。
	ListBoxes(siteLabel, workdirLabel string) ([]Box, error)
	// ListVolumes は labelKey を持つボリューム名を列挙する。
	ListVolumes(labelKey string) ([]string, error)
}

// RunOpts はバックグラウンドコンテナ起動のオプション。
type RunOpts struct {
	Name string
	// Hostname はコンテナのホスト名。バックエンドが対応しない場合は無視される
	// (Apple container はホスト名が常にコンテナ名になる)。
	Hostname string
	Image    string
	Labels   map[string]string
	Env      map[string]string
	// Volumes は "source:target" 形式のマウント指定。
	Volumes []string
	Workdir string
	Command []string
}

// BuildOpts はイメージビルドのオプション。
type BuildOpts struct {
	// NoCache はレイヤーキャッシュを無効化する。@latest 指定のパッケージを
	// 実際に更新するには必須(キャッシュが効くと古い install 層が再利用される)。
	NoCache   bool
	BuildArgs map[string]string
}

// ExecOpts はコンテナ内での対話コマンド実行のオプション。
type ExecOpts struct {
	Name    string
	Workdir string
	// User は実行ユーザー。コンテナのメインプロセスは root(tcb-boot)なので、
	// セッションは明示的に非特権ユーザーを指定する。
	User string
	// Env は追加環境変数。Apple container の exec はユーザー指定時も HOME を
	// 設定しないため、呼び出し側が明示する。
	Env     map[string]string
	Command []string
}

// Box は tcb 管理コンテナの一覧表示用情報。
type Box struct {
	Name       string
	Site       string
	State      string
	Workdir    string
	RunningFor string
}

// Runner はランタイム CLI の実行を抽象化する(テスト用に差し替え可能)。
type Runner interface {
	// Output は <bin> <args...> を実行して標準出力を返す。
	Output(args ...string) (string, error)
	// Interactive は標準入出力を引き継いで <bin> <args...> を実行する。
	Interactive(args ...string) error
}

type execRunner struct {
	bin string
}

func (r *execRunner) Output(args ...string) (string, error) {
	cmd := exec.Command(r.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s %s: %s", r.bin, args[0], msg)
	}
	return stdout.String(), nil
}

func (r *execRunner) Interactive(args ...string) error {
	cmd := exec.Command(r.bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sortedKeys は map の走査順を安定させる(CLI 引数の順序を決定的にする)。
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

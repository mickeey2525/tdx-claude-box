// Package config は tcb の既定値(イメージタグ、命名規則、workdir ルート)を持つ。
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// ImageTag は tcb が使うコンテナイメージのタグ。
	ImageTag = "tcb:latest"

	// LabelSite / LabelWorkdir / LabelTDSite は tcb 管理リソースに付けるラベルキー。
	// LabelSite は box 名(隔離の単位)、LabelTDSite は実際の TD site
	// (box 名と別にできる。例: box "us01-7060" → TD site "us01")。
	LabelSite    = "tcb.site"
	LabelWorkdir = "tcb.workdir"
	LabelTDSite  = "tcb.tdsite"

	// HomeMount はコンテナ内で site 別ボリュームをマウントする先。
	HomeMount = "/home/tcb"
	// WorkMount はホスト側作業ディレクトリのマウント先。
	WorkMount = "/work"

	// EntryCommand はコンテナ内のセッションエントリポイント。
	EntryCommand = "tcb-entry"
	// BootCommand はコンテナのメインプロセス(root で HOME を初期化して常駐)。
	BootCommand = "tcb-boot"
	// SessionUser はセッション(exec)の実行ユーザー。
	SessionUser = "tcb"
)

// ContainerName は site 用コンテナの名前を返す(1 site 1 コンテナ)。
func ContainerName(site string) string { return "tcb-" + site }

// VolumeName は site 用 HOME ボリュームの名前を返す。
func VolumeName(site string) string { return "tcb-" + site + "-home" }

// Hostname はコンテナのホスト名(プロンプト等で site が分かるように)。
func Hostname(site string) string { return site + ".tcb" }

// DefaultWorkdir は site の既定作業ディレクトリ(~/tcb/<site>)を返す。
func DefaultWorkdir(site string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "tcb", site), nil
}

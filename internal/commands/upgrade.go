package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// ModulePath は self-update に使う Go モジュールパス。
const ModulePath = "github.com/mickeey2525/tdx-claude-box"

// Upgrade は tcb 本体とコンテナイメージを更新する。
// 既定は両方: go install で新バイナリを入れ、埋め込み Dockerfile が
// 変わっている可能性があるためイメージ再ビルドは**新バイナリを子プロセスで**
// 実行する(upgrade --image-only)。
func Upgrade(backendName string, args []string, stdout io.Writer) error {
	var binaryOnly, imageOnly bool
	for _, a := range args {
		switch a {
		case "--binary-only":
			binaryOnly = true
		case "--image-only":
			imageOnly = true
		default:
			return fmt.Errorf("usage: tcb upgrade [--binary-only|--image-only]")
		}
	}
	if binaryOnly && imageOnly {
		return fmt.Errorf("--binary-only and --image-only are mutually exclusive")
	}

	if imageOnly {
		e, err := engine.Select(backendName)
		if err != nil {
			return err
		}
		if err := ensureImage(e, true); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "image updated. Existing boxes keep the old image until recreated ('tcb rm <box>' then 'tcb run <box>').")
		return nil
	}

	fmt.Fprintf(stdout, "current: %s\n", versionString())
	newBin, err := selfInstall(stdout)
	if err != nil {
		return err
	}

	// 新バイナリのバージョンを表示(失敗しても致命的ではない)
	if out, err := exec.Command(newBin, "version").Output(); err == nil {
		fmt.Fprintf(stdout, "installed: %s\n", strings.TrimSpace(string(out)))
	}

	if binaryOnly {
		return nil
	}

	rest := []string{}
	if backendName != "" {
		rest = append(rest, "--backend", backendName)
	}
	rest = append(rest, "upgrade", "--image-only")
	cmd := exec.Command(newBin, rest...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// selfInstall は go install で最新の tcb を入れ、そのパスを返す。
func selfInstall(stdout io.Writer) (string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("go toolchain not found in PATH; install Go, or update manually (git pull && go install ./cmd/tcb)")
	}

	fmt.Fprintf(stdout, "running go install %s/cmd/tcb@latest\n", ModulePath)
	cmd := exec.Command(goBin, "install", ModulePath+"/cmd/tcb@latest")
	// private リポジトリでも proxy/sumdb を経由せず直接取得できるようにする
	cmd.Env = append(os.Environ(), "GOPRIVATE="+ModulePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go install failed: %w (a private repo needs git auth, e.g. 'gh auth setup-git' or SSH)", err)
	}

	return installedBinaryPath(goBin)
}

// installedBinaryPath は go install が置いたバイナリのパスを返す。
func installedBinaryPath(goBin string) (string, error) {
	out, err := exec.Command(goBin, "env", "GOBIN").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOBIN: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		out, err = exec.Command(goBin, "env", "GOPATH").Output()
		if err != nil {
			return "", fmt.Errorf("go env GOPATH: %w", err)
		}
		dir = filepath.Join(strings.TrimSpace(string(out)), "bin")
	}
	bin := filepath.Join(dir, "tcb")
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("installed binary not found at %s: %w", bin, err)
	}
	return bin, nil
}

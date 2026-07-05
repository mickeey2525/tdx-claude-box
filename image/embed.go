// Package image は tcb コンテナイメージのビルドコンテキスト
// (Dockerfile と entrypoint.sh)をバイナリに埋め込む。
// これにより tcb 単体(go install した1バイナリ)でイメージをビルドできる。
package image

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed Dockerfile
var dockerfile []byte

//go:embed entrypoint.sh
var entrypoint []byte

//go:embed boot.sh
var boot []byte

//go:embed xdg-open
var xdgOpen []byte

// WriteBuildContext は埋め込んだビルドコンテキストを dir に書き出す。
func WriteBuildContext(dir string) error {
	files := map[string][]byte{
		"Dockerfile":    dockerfile,
		"entrypoint.sh": entrypoint,
		"boot.sh":       boot,
		"xdg-open":      xdgOpen,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return fmt.Errorf("write build context: %w", err)
		}
	}
	return nil
}

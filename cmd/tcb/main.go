// tcb は tdx claude を site ごとに隔離された Docker コンテナで動かす CLI。
package main

import (
	"os"

	"github.com/mickeey2525/tdx-claude-box/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}

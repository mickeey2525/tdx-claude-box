package commands

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"

	"github.com/mickeey2525/tdx-claude-box/internal/bridge"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// startSessionBridge はセッション用 URL ブリッジを起動し、コンテナへ渡す
// TCB_BRIDGE の値(host:port)を返す。box 内の xdg-open シムがここへ URL を
// 送ると、ホストのブラウザで開き OAuth コールバックを box へ中継する。
// ブリッジを組めなくてもセッションは従来動作(URL 表示のみ)で続行する。
func startSessionBridge(e engine.Engine, name string) (*bridge.Bridge, string) {
	bindIP, dialHost, err := e.BridgeAddrs(name)
	if err == nil {
		var b *bridge.Bridge
		b, err = bridge.Start(bridge.Config{
			BindIP: bindIP,
			Dial: func(port int) (io.ReadWriteCloser, error) {
				return e.DialContainerPort(name, port)
			},
		})
		if err == nil {
			return b, net.JoinHostPort(dialHost, strconv.Itoa(b.Port()))
		}
	}
	fmt.Fprintf(os.Stderr, "tcb: warning: URL bridge disabled (%v); browser URLs will only be printed\n", err)
	return nil, ""
}

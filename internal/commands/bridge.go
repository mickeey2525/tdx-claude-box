package commands

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/mickeey2525/tdx-claude-box/internal/bridge"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// claudeOAuthCallbackPort は Claude Code の MCP OAuth コールバックの固定ポート
// (実機で確認)。Claude Code はコンテナ等のヘッドレス環境でブラウザ起動を
// スキップするため xdg-open 経由で URL がブリッジに届かない。事前に中継を
// 張っておけば、ユーザーが端末の URL をホストブラウザで開くだけで完了する。
const claudeOAuthCallbackPort = 3118

// prearmPorts はセッション開始時に中継を張るポート一覧。
// TCB_BRIDGE_PORTS(カンマ区切り)で追加できる。
func prearmPorts() []int {
	ports := []int{claudeOAuthCallbackPort}
	for _, s := range strings.Split(os.Getenv("TCB_BRIDGE_PORTS"), ",") {
		if p, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && p > 0 && p != claudeOAuthCallbackPort {
			ports = append(ports, p)
		}
	}
	return ports
}

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
			PrearmPorts: prearmPorts(),
		})
		if err == nil {
			return b, net.JoinHostPort(dialHost, strconv.Itoa(b.Port()))
		}
	}
	fmt.Fprintf(os.Stderr, "tcb: warning: URL bridge disabled (%v); browser URLs will only be printed\n", err)
	return nil, ""
}

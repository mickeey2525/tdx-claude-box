//go:build e2e

package commands

// 実ランタイムで URL ブリッジを end-to-end 検証するオプトインテスト。
// box 内の xdg-open シム → TCB_BRIDGE → ホストのブリッジ → exec + socat の
// 中継、の全経路を通す。コールバックサーバーは実際の OAuth クライアントと
// 同じくコンテナ内の 127.0.0.1 にバインドする。
//
//	docker build -t tcb-e2e:latest image/                            # Docker
//	container build -t tcb-e2e:latest -f image/Dockerfile image/     # Apple
//	go test -tags e2e -run TestE2EBridge -count=1 ./internal/commands
//
// TCB_E2E_IMAGE でイメージタグを差し替えられる(既定 tcb-e2e:latest)。
// ランタイムが動いていないバックエンドは skip される。

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mickeey2525/tdx-claude-box/internal/bridge"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

func e2eImage() string {
	if img := os.Getenv("TCB_E2E_IMAGE"); img != "" {
		return img
	}
	return "tcb-e2e:latest"
}

func TestE2EBridgeDocker(t *testing.T) {
	e := engine.NewDocker()
	if err := e.Available(); err != nil {
		t.Skipf("docker not available: %v", err)
	}
	runBridgeE2E(t, e, "docker", []string{"rm", "-f"})
}

func TestE2EBridgeApple(t *testing.T) {
	e := engine.NewApple()
	if err := e.Available(); err != nil {
		t.Skipf("apple container not available: %v", err)
	}
	runBridgeE2E(t, e, "container", []string{"delete", "--force"})
}

func runBridgeE2E(t *testing.T, e engine.Engine, bin string, rmArgs []string) {
	const box = "tcb-e2e-bridge"
	const cbPort = 8976

	cleanup := func() { exec.Command(bin, append(rmArgs, box)...).Run() }
	cleanup()

	// コールバック受け役の http サーバーをメインプロセスとして起動
	// (実 OAuth クライアントと同様に 127.0.0.1 バインド)
	server := fmt.Sprintf(
		`require("http").createServer((q,s)=>{s.end("ok:"+q.url)}).listen(%d,"127.0.0.1",()=>console.log("up"))`, cbPort)
	if err := e.RunDetached(engine.RunOpts{
		Name:    box,
		Image:   e2eImage(),
		Command: []string{"node", "-e", server},
	}); err != nil {
		t.Fatalf("start container: %v", err)
	}
	t.Cleanup(cleanup)

	// Apple container は VM 起動に少し時間がかかる。networks が出るまで待つ
	var bindIP, dialHost string
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		bindIP, dialHost, err = e.BridgeAddrs(box)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("BridgeAddrs: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// セッションと同じ構成でブリッジを起動(ブラウザは開かず記録する)。
	// cbPort を PrearmPorts に入れることで、コールバックが共有ポートの
	// 振り分け(probe: 先頭チャンク書き込み→応答確認)を通ることも検証する。
	opened := make(chan string, 1)
	b, err := bridge.Start(bridge.Config{
		BindIP:      bindIP,
		Dial:        func(port int) (io.ReadWriteCloser, error) { return e.DialContainerPort(box, port) },
		Open:        func(u string) error { opened <- u; return nil },
		Log:         os.Stderr,
		PrearmPorts: []int{cbPort},
	})
	if err != nil {
		t.Fatalf("bridge start: %v", err)
	}
	defer b.Close()
	addr := net.JoinHostPort(dialHost, strconv.Itoa(b.Port()))

	// box 内から xdg-open シムを実行(tdx/claude が URL を開く場面の再現)。
	// コンテナ起動直後はサーバー・ネットワークが立ち上がりきっていないことが
	// あるためリトライする
	authURL := fmt.Sprintf(
		"https://example.com/auth?redirect_uri=http%%3A%%2F%%2Flocalhost%%3A%d%%2Fcb", cbPort)
	var out []byte
	deadline = time.Now().Add(30 * time.Second)
	for {
		out, err = exec.Command(bin, "exec", "--env", "TCB_BRIDGE="+addr,
			box, "xdg-open", authURL).CombinedOutput()
		if err == nil && strings.Contains(string(out), "opening in host browser") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shim did not reach the bridge: %v\n%s", err, out)
		}
		time.Sleep(1 * time.Second)
	}

	select {
	case u := <-opened:
		if u != authURL {
			t.Errorf("opened %q, want %q", u, authURL)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not receive the URL")
	}

	// ホストブラウザのコールバック相当: localhost:<cbPort> → box 内サーバー
	var body []byte
	deadline = time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/cb?code=abc", cbPort))
		if err == nil {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("callback via relay failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := string(body); got != "ok:/cb?code=abc" {
		t.Errorf("callback response = %q, want ok:/cb?code=abc", got)
	}
}

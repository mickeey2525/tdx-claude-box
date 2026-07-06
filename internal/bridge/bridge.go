// Package bridge は box 内から届いた URL をホストのブラウザで開き、
// OAuth の localhost コールバックをコンテナ内のポートへ中継する。
//
// コンテナ内の xdg-open シムが TCB_BRIDGE(host:port)宛てに URL を1行送ると、
// ブリッジは URL(および redirect_uri クエリ)が名指しする localhost ポートに
// ホスト側リスナーを張り、以後そのポートへの接続を box 内へ転送する。
// これで「コールバック先ポートが事前に分からない」問題が消える。
package bridge

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Dialer はコンテナ内 127.0.0.1:port への接続を開く(engine 側が提供する)。
type Dialer func(port int) (io.ReadWriteCloser, error)

// Config は Start のオプション。
type Config struct {
	// BindIP はブリッジリスナーのバインド先(Docker: 127.0.0.1、Apple: vmnet GW)。
	BindIP string
	Dial   Dialer
	// Open は URL をブラウザで開く。nil なら macOS の open コマンド。
	Open func(url string) error
	// Log は警告・進捗の出力先。nil なら os.Stderr。
	Log io.Writer
}

// Bridge は1セッション分の URL ブリッジ。Close まで動き続ける。
type Bridge struct {
	cfg Config
	ln  net.Listener

	mu     sync.Mutex
	relays map[int]net.Listener // コールバックポート → ホスト側リスナー(重複排除)
}

// Start はブリッジリスナーを起動する(ポートは自動採番)。
func Start(cfg Config) (*Bridge, error) {
	if cfg.Dial == nil {
		return nil, fmt.Errorf("bridge: Dial is required")
	}
	if cfg.Open == nil {
		cfg.Open = func(u string) error { return exec.Command("open", u).Run() }
	}
	if cfg.Log == nil {
		cfg.Log = os.Stderr
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.BindIP, "0"))
	if err != nil {
		return nil, fmt.Errorf("bridge: listen on %s: %w", cfg.BindIP, err)
	}
	b := &Bridge{cfg: cfg, ln: ln, relays: map[int]net.Listener{}}
	go b.acceptLoop()
	return b, nil
}

// Port はブリッジリスナーのポート番号を返す。
func (b *Bridge) Port() int {
	return b.ln.Addr().(*net.TCPAddr).Port
}

// Close はブリッジ本体と全中継リスナーを閉じる。転送中の接続は自然終了に任せる
// (呼び出し元の tcb プロセスは直後に終了する)。
func (b *Bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ln := range b.relays {
		ln.Close()
	}
	b.relays = map[int]net.Listener{}
	return b.ln.Close()
}

func (b *Bridge) acceptLoop() {
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return // Close() された
		}
		go b.handle(conn)
	}
}

// handle は1接続=1 URL 行のプロトコルを処理する。
func (b *Bridge) handle(c net.Conn) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReaderSize(c, 8192).ReadString('\n')
	if err != nil && line == "" {
		return
	}
	raw := strings.TrimSpace(line)
	u, err := url.Parse(raw)
	// box 内プロセスに任意スキームを開かせない(open-anything 防止)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fmt.Fprintf(b.cfg.Log, "tcb: bridge: ignoring non-http URL from box\n")
		return
	}
	for _, port := range callbackPorts(u) {
		b.ensureRelay(port)
	}
	fmt.Fprintf(b.cfg.Log, "tcb: opening in host browser: %s\n", u.String())
	if err := b.cfg.Open(u.String()); err != nil {
		fmt.Fprintf(b.cfg.Log, "tcb: warning: could not open browser (%v); open manually:\ntcb:   %s\n", err, u.String())
	}
}

// callbackPorts は URL が名指しする localhost のコールバックポートを集める。
// ① URL 自体が localhost + 明示ポート(ツールが自前のローカルページを開く場合)
// ② redirect_uri クエリが localhost + 明示ポート(OAuth 認可 URL)
// デフォルトポート(80/443)は特権ポートかつ OAuth の一時ポート慣行に合わないため対象外。
func callbackPorts(u *url.URL) []int {
	var ports []int
	seen := map[int]bool{}
	add := func(target *url.URL) {
		if target == nil || !isLoopbackHost(target.Hostname()) {
			return
		}
		var port int
		if _, err := fmt.Sscanf(target.Port(), "%d", &port); err != nil || port <= 0 {
			return
		}
		if !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}
	add(u)
	if ru := u.Query().Get("redirect_uri"); ru != "" {
		if parsed, err := url.Parse(ru); err == nil {
			add(parsed)
		}
	}
	return ports
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ensureRelay は port へのホスト側中継リスナーを(なければ)起動する。
// ブラウザのリダイレクト先は常に localhost なので、バインド先は
// バックエンドによらずホストの loopback で良い。
func (b *Bridge) ensureRelay(port int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.relays[port]; ok {
		return
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(b.cfg.Log, "tcb: warning: cannot forward port %d (%v); opening the URL anyway\n", port, err)
		return
	}
	b.relays[port] = ln
	fmt.Fprintf(b.cfg.Log, "tcb: forwarding 127.0.0.1:%d -> box:%d\n", port, port)
	go b.relayLoop(ln, port)
}

func (b *Bridge) relayLoop(ln net.Listener, port int) {
	for {
		hostConn, err := ln.Accept()
		if err != nil {
			return // Close() された
		}
		go func() {
			defer hostConn.Close()
			boxConn, err := b.cfg.Dial(port)
			if err != nil {
				fmt.Fprintf(b.cfg.Log, "tcb: warning: relay to box port %d failed: %v\n", port, err)
				return
			}
			defer boxConn.Close()
			go func() {
				io.Copy(boxConn, hostConn)
				closeWrite(boxConn)
			}()
			// box からの応答が終わったら両方向とも閉じる(defer)。forward 側の
			// goroutine はブラウザが接続を握ったままでも Close で確実に解ける。
			io.Copy(hostConn, boxConn)
			closeWrite(hostConn)
		}()
	}
}

// closeWrite は書き込み側だけ閉じられるなら閉じる(HTTP keep-alive クライアント
// 相手でも EOF が伝わるように)。*net.TCPConn と engine の streamProc が該当。
func closeWrite(c io.Closer) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
}

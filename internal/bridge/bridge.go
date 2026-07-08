// Package bridge は box 内から届いた URL をホストのブラウザで開き、
// OAuth の localhost コールバックをコンテナ内のポートへ中継する。
//
// 経路は2つある:
//  1. コンテナ内の xdg-open シムが TCB_BRIDGE(host:port)宛てに URL を1行
//     送ると、URL(および redirect_uri クエリ)が名指しする localhost ポートに
//     中継リスナーを張ってからホストのブラウザで開く。
//  2. Claude Code のようにヘッドレス環境でブラウザ起動をスキップするツールの
//     ために、既知の固定コールバックポート(PrearmPorts)はセッション開始時
//     から中継を張る。このポートはホスト上で全 box が取り合いになるため
//     「共有ポート」として扱い、接続ごとに実際に待ち受けている box へ
//     振り分ける(コールバックサーバーは認証中しか listen しない性質を使う)。
//     取れなかったセッションは定期的にバインドを再試行し、保持セッションの
//     終了後に引き継ぐ。
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
	// Dial は自 box への接続を開く。動的中継はこれのみを使い、
	// 共有ポートの振り分けでは最優先候補になる。
	Dial Dialer
	// Peers は共有ポートの振り分け先候補(自分以外の実行中 box)を返す。
	// 接続ごとに呼ばれる。nil なら自 box のみ。
	Peers func() []Dialer
	// Open は URL をブラウザで開く。nil なら macOS の open コマンド。
	Open func(url string) error
	// Log は警告・進捗の出力先。nil なら os.Stderr。
	Log io.Writer
	// PrearmPorts はセッション開始時から中継を張っておく共有コールバック
	// ポート(例: Claude Code の MCP OAuth は 3118 固定)。
	PrearmPorts []int
	// RetryInterval は共有ポートのバインド再試行間隔。0 なら 15 秒。
	RetryInterval time.Duration
}

// Bridge は1セッション分の URL ブリッジ。Close まで動き続ける。
type Bridge struct {
	cfg  Config
	ln   net.Listener
	done chan struct{}

	mu     sync.Mutex
	relays map[int]net.Listener // コールバックポート → ホスト側リスナー(重複排除)
	shared map[int]bool         // 共有ポート(振り分け対象)か
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
	if cfg.RetryInterval == 0 {
		cfg.RetryInterval = 15 * time.Second
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.BindIP, "0"))
	if err != nil {
		return nil, fmt.Errorf("bridge: listen on %s: %w", cfg.BindIP, err)
	}
	b := &Bridge{
		cfg:    cfg,
		ln:     ln,
		done:   make(chan struct{}),
		relays: map[int]net.Listener{},
		shared: map[int]bool{},
	}
	var pending []int
	for _, port := range cfg.PrearmPorts {
		if !b.ensureRelay(port, true) {
			fmt.Fprintf(b.cfg.Log,
				"tcb: note: callback port %d is held by another tcb session; will take over when free\n", port)
			pending = append(pending, port)
		}
	}
	if len(pending) > 0 {
		go b.rebindLoop(pending)
	}
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
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	for _, ln := range b.relays {
		ln.Close()
	}
	b.relays = map[int]net.Listener{}
	return b.ln.Close()
}

// rebindLoop は他セッションに取られていた共有ポートのバインドを再試行する。
func (b *Bridge) rebindLoop(ports []int) {
	ticker := time.NewTicker(b.cfg.RetryInterval)
	defer ticker.Stop()
	for len(ports) > 0 {
		select {
		case <-b.done:
			return
		case <-ticker.C:
		}
		var remain []int
		for _, port := range ports {
			if !b.ensureRelay(port, true) {
				remain = append(remain, port)
			}
		}
		ports = remain
	}
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
		if !b.ensureRelay(port, false) {
			fmt.Fprintf(b.cfg.Log,
				"tcb: warning: cannot forward port %d (in use); opening the URL anyway\n", port)
		}
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
// バックエンドによらずホストの loopback で良い。成功(または既存)で true。
func (b *Bridge) ensureRelay(port int, shared bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.done:
		return false
	default:
	}
	if _, ok := b.relays[port]; ok {
		return true
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	b.relays[port] = ln
	b.shared[port] = shared
	fmt.Fprintf(b.cfg.Log, "tcb: forwarding 127.0.0.1:%d -> box:%d\n", port, port)
	go b.relayLoop(ln, port, shared)
	return true
}

func (b *Bridge) relayLoop(ln net.Listener, port int, shared bool) {
	for {
		hostConn, err := ln.Accept()
		if err != nil {
			return // Close() された
		}
		go b.serve(hostConn, port, shared)
	}
}

// serve は中継1接続を処理する。動的ポート(URL 由来)は自 box へ直結、
// 共有ポートは待ち受けている box を探して振り分ける。
func (b *Bridge) serve(hostConn net.Conn, port int, shared bool) {
	defer hostConn.Close()
	if !shared {
		boxConn, err := b.cfg.Dial(port)
		if err != nil {
			fmt.Fprintf(b.cfg.Log, "tcb: warning: relay to box port %d failed: %v\n", port, err)
			return
		}
		splice(hostConn, boxConn)
		return
	}

	// 共有ポート: リクエストの先頭チャンクを読み、応答を返した box に流す。
	// OAuth コールバックは小さな GET なので先頭チャンクにヘッダが収まる。
	buf := make([]byte, 8192)
	hostConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := hostConn.Read(buf)
	if n == 0 {
		_ = err
		return
	}
	hostConn.SetReadDeadline(time.Time{})

	candidates := []Dialer{b.cfg.Dial}
	if b.cfg.Peers != nil {
		candidates = append(candidates, b.cfg.Peers()...)
	}
	for _, dial := range candidates {
		boxConn, err := dial(port)
		if err != nil {
			continue
		}
		if first, ok := probe(boxConn, buf[:n]); ok {
			if _, err := hostConn.Write(first); err != nil {
				boxConn.Close()
				return
			}
			splice(hostConn, boxConn)
			return
		}
		boxConn.Close()
	}
	fmt.Fprintf(b.cfg.Log, "tcb: warning: no box is listening on callback port %d\n", port)
}

// probe はリクエストの先頭チャンクを box へ書き、応答の先頭バイトが返るかで
// その box が実際に待ち受けているか判定する(接続拒否は EOF になる)。
func probe(boxConn io.ReadWriteCloser, chunk []byte) ([]byte, bool) {
	if _, err := boxConn.Write(chunk); err != nil {
		return nil, false
	}
	resp := make([]byte, 8192)
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := boxConn.Read(resp)
		ch <- result{n, err}
	}()
	select {
	case r := <-ch:
		return resp[:r.n], r.n > 0
	case <-time.After(2 * time.Second):
		// 呼び出し元が boxConn を Close して読み取り goroutine を解く
		return nil, false
	}
}

// splice は両方向をコピーし、half-close を伝播する。hostConn の Close は
// 呼び出し元(serve の defer)が行う。
func splice(hostConn net.Conn, boxConn io.ReadWriteCloser) {
	defer boxConn.Close()
	go func() {
		io.Copy(boxConn, hostConn)
		closeWrite(boxConn)
	}()
	// box からの応答が終わったら両方向とも閉じる(defer)。forward 側の
	// goroutine はブラウザが接続を握ったままでも Close で確実に解ける。
	io.Copy(hostConn, boxConn)
	closeWrite(hostConn)
}

// closeWrite は書き込み側だけ閉じられるなら閉じる(HTTP keep-alive クライアント
// 相手でも EOF が伝わるように)。*net.TCPConn と engine の streamProc が該当。
func closeWrite(c io.Closer) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
}

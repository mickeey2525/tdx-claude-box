package bridge

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer は goroutine から書かれるログ用のスレッドセーフなバッファ。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// sendURL はシムと同じプロトコルでブリッジへ URL を1行送る。
func sendURL(t *testing.T, port int, url string) {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "%s\n", url); err != nil {
		t.Fatalf("send url: %v", err)
	}
}

// freePort は空いている TCP ポートを見つける。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func waitOpened(t *testing.T, opened chan string) string {
	t.Helper()
	select {
	case u := <-opened:
		return u
	case <-time.After(5 * time.Second):
		t.Fatal("opener was not called within 5s")
		return ""
	}
}

func TestCallbackPorts(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want []int
	}{
		{
			name: "encoded redirect_uri",
			url:  "https://auth.example.com/authorize?client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A54545%2Fcallback",
			want: []int{54545},
		},
		{
			name: "opened URL itself is localhost",
			url:  "http://localhost:8976/setup",
			want: []int{8976},
		},
		{
			name: "both, deduped when equal",
			url:  "http://127.0.0.1:8976/auth?redirect_uri=http%3A%2F%2Flocalhost%3A8976%2Fcb",
			want: []int{8976},
		},
		{
			name: "both, distinct ports",
			url:  "http://localhost:3000/auth?redirect_uri=http%3A%2F%2F127.0.0.1%3A4000%2Fcb",
			want: []int{3000, 4000},
		},
		{
			name: "non-local redirect_uri ignored",
			url:  "https://auth.example.com/authorize?redirect_uri=https%3A%2F%2Fapp.example.com%2Fcb",
			want: nil,
		},
		{
			name: "no explicit port ignored",
			url:  "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%2Fcb",
			want: nil,
		},
		{
			name: "plain remote URL",
			url:  "https://example.com/docs",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := callbackPorts(u)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("callbackPorts(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestBridgeOpensURL(t *testing.T) {
	opened := make(chan string, 1)
	b, err := Start(Config{
		BindIP: "127.0.0.1",
		Dial:   func(port int) (io.ReadWriteCloser, error) { return nil, fmt.Errorf("unused") },
		Open:   func(u string) error { opened <- u; return nil },
		Log:    &syncBuffer{},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	sendURL(t, b.Port(), "https://example.com/docs")
	if got := waitOpened(t, opened); got != "https://example.com/docs" {
		t.Errorf("opened %q, want https://example.com/docs", got)
	}
}

func TestBridgeIgnoresNonHTTP(t *testing.T) {
	opened := make(chan string, 1)
	log := &syncBuffer{}
	b, err := Start(Config{
		BindIP: "127.0.0.1",
		Dial:   func(port int) (io.ReadWriteCloser, error) { return nil, fmt.Errorf("unused") },
		Open:   func(u string) error { opened <- u; return nil },
		Log:    log,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	for _, bad := range []string{"file:///etc/passwd", "garbage", ""} {
		sendURL(t, b.Port(), bad)
	}
	// 拒否ログが出るまで待ってから opener が呼ばれていないことを確認
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(log.String(), "ignoring non-http URL") < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("rejection logs not seen; log: %q", log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case u := <-opened:
		t.Errorf("opener should not be called, got %q", u)
	default:
	}
}

func TestBridgeRelayEndToEnd(t *testing.T) {
	// box 側のふり: loopback の echo サーバー。net.Pipe は CloseWrite
	// (half-close)を持たず EOF が伝播しないため、実物と同じ TCP を使う。
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	defer echoLn.Close()
	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn) // クライアントの half-close まで echo
			}()
		}
	}()
	dial := func(port int) (io.ReadWriteCloser, error) {
		return net.Dial("tcp", echoLn.Addr().String())
	}
	opened := make(chan string, 1)
	b, err := Start(Config{
		BindIP: "127.0.0.1",
		Dial:   dial,
		Open:   func(u string) error { opened <- u; return nil },
		Log:    &syncBuffer{},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	port := freePort(t)
	sendURL(t, b.Port(), fmt.Sprintf(
		"https://auth.example.com/authorize?redirect_uri=http%%3A%%2F%%2Flocalhost%%3A%d%%2Fcb", port))
	waitOpened(t, opened) // opener が呼ばれた時点で中継リスナーは張られている

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer conn.Close()
	msg := "GET /cb?code=abc HTTP/1.1\r\n"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != msg {
		t.Errorf("echo roundtrip = %q, want %q", got, msg)
	}
}

func TestBridgeRelayDedupe(t *testing.T) {
	opened := make(chan string, 2)
	log := &syncBuffer{}
	b, err := Start(Config{
		BindIP: "127.0.0.1",
		Dial:   func(port int) (io.ReadWriteCloser, error) { return nil, fmt.Errorf("unused") },
		Open:   func(u string) error { opened <- u; return nil },
		Log:    log,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	port := freePort(t)
	u := fmt.Sprintf("http://localhost:%d/setup", port)
	sendURL(t, b.Port(), u)
	waitOpened(t, opened)
	sendURL(t, b.Port(), u)
	waitOpened(t, opened)

	if n := strings.Count(log.String(), "forwarding"); n != 1 {
		t.Errorf("forwarding logged %d times, want 1 (dedupe); log: %q", n, log.String())
	}
}

func TestBridgePortInUseStillOpens(t *testing.T) {
	// コールバックポートを先に塞いでおく
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port

	opened := make(chan string, 1)
	log := &syncBuffer{}
	b, err := Start(Config{
		BindIP: "127.0.0.1",
		Dial:   func(port int) (io.ReadWriteCloser, error) { return nil, fmt.Errorf("unused") },
		Open:   func(u string) error { opened <- u; return nil },
		Log:    log,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	sendURL(t, b.Port(), fmt.Sprintf("http://localhost:%d/setup", port))
	waitOpened(t, opened)
	if !strings.Contains(log.String(), "cannot forward port") {
		t.Errorf("expected port-in-use warning; log: %q", log.String())
	}
}

func TestBridgePrearmPorts(t *testing.T) {
	// URL が届く前(=ヘッドレスでブラウザ起動がスキップされるケース)でも
	// 事前指定のポートには中継が張られている
	port := freePort(t)
	log := &syncBuffer{}
	b, err := Start(Config{
		BindIP:      "127.0.0.1",
		Dial:        func(port int) (io.ReadWriteCloser, error) { return nil, fmt.Errorf("unused") },
		Open:        func(u string) error { return nil },
		Log:         log,
		PrearmPorts: []int{port},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("prearmed relay not listening: %v", err)
	}
	conn.Close()
	if !strings.Contains(log.String(), "forwarding") {
		t.Errorf("expected forwarding log; got %q", log.String())
	}
}

// deadStream は「接続拒否された box」を装う(書けるが応答なしで即 EOF)。
type deadStream struct{}

func (deadStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (deadStream) Write(p []byte) (int, error) { return len(p), nil }
func (deadStream) Close() error                { return nil }

// startEcho は loopback の echo サーバーを立てて、その Dialer を返す。
func startEcho(t *testing.T) Dialer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return func(port int) (io.ReadWriteCloser, error) {
		return net.Dial("tcp", ln.Addr().String())
	}
}

// roundtrip は port へ HTTP リクエスト風のチャンクを送り、応答を全部読む。
func roundtrip(t *testing.T, port int, msg string) (string, error) {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(msg)); err != nil {
		return "", err
	}
	conn.(*net.TCPConn).CloseWrite()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(conn)
	return string(got), err
}

func TestSharedRelayRoutesToListeningBox(t *testing.T) {
	// 自 box は待ち受けていない(dead)、別の box が待ち受けている状況で、
	// 共有ポートへのコールバックが別 box に届くこと(先頭チャンクの再送込み)
	peer := startEcho(t)
	port := freePort(t)
	b, err := Start(Config{
		BindIP:      "127.0.0.1",
		Dial:        func(port int) (io.ReadWriteCloser, error) { return deadStream{}, nil },
		Peers:       func() []Dialer { return []Dialer{peer} },
		Open:        func(u string) error { return nil },
		Log:         &syncBuffer{},
		PrearmPorts: []int{port},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	msg := "GET /cb?code=abc HTTP/1.1\r\n\r\n"
	got, err := roundtrip(t, port, msg)
	if err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if got != msg {
		t.Errorf("routed response = %q, want %q", got, msg)
	}
}

func TestSharedRelayTakeover(t *testing.T) {
	// 共有ポートを他セッション(相当)が握っている間は再試行し、
	// 解放されたら引き継いで自 box へ中継できること
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("holder listen: %v", err)
	}
	port := holder.Addr().(*net.TCPAddr).Port

	log := &syncBuffer{}
	b, err := Start(Config{
		BindIP:        "127.0.0.1",
		Dial:          startEcho(t),
		Open:          func(u string) error { return nil },
		Log:           log,
		PrearmPorts:   []int{port},
		RetryInterval: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()
	if !strings.Contains(log.String(), "held by another") {
		t.Errorf("expected busy note; log: %q", log.String())
	}

	holder.Close()
	msg := "GET /cb HTTP/1.1\r\n\r\n"
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := roundtrip(t, port, msg)
		if err == nil && got == msg {
			return // 引き継ぎ完了
		}
		if time.Now().After(deadline) {
			t.Fatalf("takeover did not happen: got %q, err %v", got, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBridgeClose(t *testing.T) {
	opened := make(chan string, 1)
	b, err := Start(Config{
		BindIP: "127.0.0.1",
		Dial:   func(port int) (io.ReadWriteCloser, error) { return nil, fmt.Errorf("unused") },
		Open:   func(u string) error { opened <- u; return nil },
		Log:    &syncBuffer{},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	port := freePort(t)
	bridgePort := b.Port()
	sendURL(t, bridgePort, fmt.Sprintf("http://localhost:%d/setup", port))
	waitOpened(t, opened)

	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", bridgePort)); err == nil {
		t.Error("bridge listener should refuse connections after Close")
	}
	if _, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		t.Error("relay listener should refuse connections after Close")
	}
}

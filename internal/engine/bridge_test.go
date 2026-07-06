package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDockerBridgeAddrs(t *testing.T) {
	d := NewDockerWithRunner(&fakeRunner{})
	bind, dial, err := d.BridgeAddrs("tcb-ap01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bind != "127.0.0.1" || dial != "host.docker.internal" {
		t.Errorf("addrs = %q, %q; want 127.0.0.1, host.docker.internal", bind, dial)
	}
}

func TestDockerDialContainerPortArgs(t *testing.T) {
	r := &fakeRunner{}
	d := NewDockerWithRunner(r)
	conn, err := d.DialContainerPort("tcb-ap01", 33418)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer conn.Close()
	// 初回は socat プリフライトが先に走る
	preflight := []string{"exec", "tcb-ap01", "socat", "-V"}
	stream := []string{"exec", "--interactive", "tcb-ap01", "socat", "STDIO", "TCP:127.0.0.1:33418"}
	if len(r.calls) != 2 || !reflect.DeepEqual(r.calls[0], preflight) || !reflect.DeepEqual(r.calls[1], stream) {
		t.Errorf("calls:\n got %v\nwant [%v %v]", r.calls, preflight, stream)
	}

	// 2回目はプリフライトなしで直接ストリームを開く
	r.calls = nil
	conn2, err := d.DialContainerPort("tcb-ap01", 33418)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer conn2.Close()
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], stream) {
		t.Errorf("second dial calls = %v, want [%v]", r.calls, stream)
	}
}

func TestDockerDialContainerPortWithoutSocat(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "", errors.New("docker exec: exit status 1")
	}}
	d := NewDockerWithRunner(r)
	_, err := d.DialContainerPort("tcb-ap01", 33418)
	if err == nil || !strings.Contains(err.Error(), "--rebuild") {
		t.Errorf("err = %v, want socat missing error with --rebuild hint", err)
	}
}

func TestAppleBridgeAddrs(t *testing.T) {
	// vmnet ゲートウェイ IP にバインドし、コンテナも同じ IP へ接続する
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleInspectJSON, nil
	}}
	a := NewAppleWithRunner(r)
	bind, dial, err := a.BridgeAddrs("tcb-ap01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bind != "192.168.64.1" || dial != "192.168.64.1" {
		t.Errorf("addrs = %q, %q; want gateway 192.168.64.1 for both", bind, dial)
	}
}

func TestAppleBridgeAddrsNoNetworks(t *testing.T) {
	// appleInspectJSONv1 は networks が空 → ブリッジは組めない
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleInspectJSONv1, nil
	}}
	a := NewAppleWithRunner(r)
	_, _, err := a.BridgeAddrs("tcb-ap01")
	if err == nil || !strings.Contains(err.Error(), "no network info") {
		t.Errorf("err = %v, want no network info error", err)
	}
}

func TestAppleDialContainerPortArgs(t *testing.T) {
	// コンテナ内 loopback バインドのサーバーへ届くよう exec + socat で入る
	// (コンテナ IP への直接 TCP では 127.0.0.1 バインドに届かない)
	r := &fakeRunner{}
	a := NewAppleWithRunner(r)
	conn, err := a.DialContainerPort("tcb-ap01", 8976)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer conn.Close()
	preflight := []string{"exec", "tcb-ap01", "socat", "-V"}
	stream := []string{"exec", "--interactive", "tcb-ap01", "socat", "STDIO", "TCP:127.0.0.1:8976"}
	if len(r.calls) != 2 || !reflect.DeepEqual(r.calls[0], preflight) || !reflect.DeepEqual(r.calls[1], stream) {
		t.Errorf("calls:\n got %v\nwant [%v %v]", r.calls, preflight, stream)
	}
}

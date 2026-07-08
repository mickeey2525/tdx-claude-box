package commands

import (
	"strings"
	"testing"

	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// execEnvValues は記録された exec 呼び出しから --env の値を集める。
func execEnvValues(r *fakeRunner) []string {
	var envs []string
	for _, call := range r.calls {
		if len(call) == 0 || call[0] != "exec" {
			continue
		}
		for i, arg := range call {
			if arg == "--env" && i+1 < len(call) {
				envs = append(envs, call[i+1])
			}
		}
	}
	return envs
}

func TestShellInjectsBridgeEnv(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	if err := Shell(e, []string{"ap01"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	envs := execEnvValues(r)
	var hasHome, hasBridge bool
	for _, env := range envs {
		if env == "HOME=/home/tcb" {
			hasHome = true
		}
		// ブリッジポートは自動採番なのでプレフィックスで確認する
		if strings.HasPrefix(env, "TCB_BRIDGE=host.docker.internal:") {
			hasBridge = true
		}
	}
	if !hasHome || !hasBridge {
		t.Errorf("exec env = %v, want HOME and TCB_BRIDGE=host.docker.internal:<port>", envs)
	}
}

func TestShellDegradesWhenBridgeUnavailable(t *testing.T) {
	// networks が空の Apple container(1.0 系)では ブリッジは組めないが、
	// セッション自体は従来どおり開始される。
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return `[{"status":{"state":"running","networks":[]},"configuration":{"labels":{},"id":"tcb-ap01"}}]`, nil
	}}
	e := engine.NewAppleWithRunner(r)
	if err := Shell(e, []string{"ap01"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.called("exec") {
		t.Fatalf("session exec should still run; calls: %v", r.calls)
	}
	for _, env := range execEnvValues(r) {
		if strings.HasPrefix(env, "TCB_BRIDGE=") {
			t.Errorf("TCB_BRIDGE must not be set when the bridge is unavailable; env: %v", env)
		}
	}
}

func TestPrearmPorts(t *testing.T) {
	t.Setenv("TCB_BRIDGE_PORTS", "")
	if got := prearmPorts(); len(got) != 1 || got[0] != 3118 {
		t.Errorf("prearmPorts() = %v, want [3118]", got)
	}
	// TCB_BRIDGE_PORTS で追加。重複・不正値は無視される
	t.Setenv("TCB_BRIDGE_PORTS", "8080, 3118, abc, -1")
	if got := prearmPorts(); len(got) != 2 || got[0] != 3118 || got[1] != 8080 {
		t.Errorf("prearmPorts() = %v, want [3118 8080]", got)
	}
}

func TestPeerDialersExcludesSelfAndStopped(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		if args[0] == "ps" {
			return "tcb-a\tap01\trunning\t/w\t2 days\n" +
				"tcb-b\tus01\trunning\t/w\t1 day\n" +
				"tcb-c\tus02\texited\t/w\t1 day\n", nil
		}
		return "", nil
	}}
	e := engine.NewDockerWithRunner(r)
	dialers := peerDialers(e, "tcb-a")
	if len(dialers) != 1 {
		t.Fatalf("len(dialers) = %d, want 1 (self and stopped excluded)", len(dialers))
	}
	// 唯一の候補は tcb-b への接続であること
	conn, err := dialers[0](3118)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer conn.Close()
	if !r.called("exec", "--interactive", "tcb-b") {
		t.Errorf("dialer should target tcb-b; calls: %v", r.calls)
	}
}

func TestStartSessionBridgeNilOnError(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return `[{"status":{"state":"running","networks":[]},"configuration":{"labels":{},"id":"tcb-ap01"}}]`, nil
	}}
	e := engine.NewAppleWithRunner(r)
	b, addr := startSessionBridge(e, "tcb-ap01")
	if b != nil || addr != "" {
		t.Errorf("startSessionBridge = %v, %q; want nil, empty on error", b, addr)
	}
}

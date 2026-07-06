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

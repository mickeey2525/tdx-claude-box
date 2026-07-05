package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// fakeRunner は commands 層のテスト用に docker 呼び出しを記録する。
type fakeRunner struct {
	calls    [][]string
	onOutput func(args []string) (string, error)
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.onOutput != nil {
		return f.onOutput(args)
	}
	return "", nil
}

func (f *fakeRunner) Interactive(args ...string) error {
	f.calls = append(f.calls, args)
	return nil
}

func (f *fakeRunner) called(prefix ...string) bool {
	for _, call := range f.calls {
		if len(call) < len(prefix) {
			continue
		}
		match := true
		for i, p := range prefix {
			if call[i] != p {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// existingBoxRunner は「tcb-ap01 が実行中・ボリュームあり」の状態を返す。
func existingBoxRunner() *fakeRunner {
	r := &fakeRunner{}
	r.onOutput = func(args []string) (string, error) {
		switch {
		case args[0] == "container" && args[1] == "inspect":
			return "running\n", nil
		case args[0] == "volume" && args[1] == "inspect":
			return "ap01\n", nil
		}
		return "", nil
	}
	return r
}

func TestRmVolumesRequiresConfirmation(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	var out bytes.Buffer

	err := Rm(e, []string{"ap01", "--volumes"}, strings.NewReader("nope\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("err = %v, want aborted", err)
	}
	if r.called("rm") || r.called("volume", "rm") {
		t.Errorf("nothing should be removed on aborted confirmation; calls: %v", r.calls)
	}
}

func TestRmVolumesConfirmed(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	var out bytes.Buffer

	err := Rm(e, []string{"ap01", "--volumes"}, strings.NewReader("ap01\n"), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.called("rm", "--force", "tcb-ap01") {
		t.Errorf("container should be removed; calls: %v", r.calls)
	}
	if !r.called("volume", "rm", "tcb-ap01-home") {
		t.Errorf("volume should be removed; calls: %v", r.calls)
	}
}

func TestRmWithoutVolumesKeepsVolume(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	var out bytes.Buffer

	err := Rm(e, []string{"ap01"}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.called("rm", "--force", "tcb-ap01") {
		t.Errorf("container should be removed; calls: %v", r.calls)
	}
	if r.called("volume", "rm") {
		t.Errorf("volume must be kept without --volumes; calls: %v", r.calls)
	}
	if !strings.Contains(out.String(), "kept volume") {
		t.Errorf("output should mention kept volume; got %q", out.String())
	}
}

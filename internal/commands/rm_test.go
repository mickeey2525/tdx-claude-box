package commands

import (
	"bytes"
	"io"
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

func (f *fakeRunner) Stream(args ...string) (io.ReadWriteCloser, error) {
	f.calls = append(f.calls, args)
	return nopStream{}, nil
}

// nopStream はテスト用の何もしない双方向ストリーム。
type nopStream struct{}

func (nopStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (nopStream) Write(p []byte) (int, error) { return len(p), nil }
func (nopStream) Close() error                { return nil }

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

func TestRmDeletesVolumeAfterConfirmation(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	var out bytes.Buffer

	err := Rm(e, []string{"ap01"}, strings.NewReader("y\n"), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.called("rm", "--force", "tcb-ap01") {
		t.Errorf("container should be removed; calls: %v", r.calls)
	}
	if !r.called("volume", "rm", "tcb-ap01-home") {
		t.Errorf("volume should be removed by default; calls: %v", r.calls)
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("warning should be shown before deleting the volume; got %q", out.String())
	}
}

func TestRmAbortsWithoutConfirmation(t *testing.T) {
	for _, input := range []string{"n\n", "\n", ""} {
		r := existingBoxRunner()
		e := engine.NewDockerWithRunner(r)
		var out bytes.Buffer

		err := Rm(e, []string{"ap01"}, strings.NewReader(input), &out)
		if err == nil || !strings.Contains(err.Error(), "aborted") {
			t.Fatalf("input %q: err = %v, want aborted", input, err)
		}
		if r.called("rm") || r.called("volume", "rm") {
			t.Errorf("input %q: nothing should be removed; calls: %v", input, r.calls)
		}
	}
}

func TestRmKeepVolume(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	var out bytes.Buffer

	// --keep-volume は認証情報を消さないので確認プロンプトなしで進む
	err := Rm(e, []string{"ap01", "--keep-volume"}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.called("rm", "--force", "tcb-ap01") {
		t.Errorf("container should be removed; calls: %v", r.calls)
	}
	if r.called("volume", "rm") {
		t.Errorf("volume must be kept with --keep-volume; calls: %v", r.calls)
	}
	if !strings.Contains(out.String(), "kept volume") {
		t.Errorf("output should mention kept volume; got %q", out.String())
	}
}

func TestRmForceSkipsPrompt(t *testing.T) {
	r := existingBoxRunner()
	e := engine.NewDockerWithRunner(r)
	var out bytes.Buffer

	err := Rm(e, []string{"ap01", "--force"}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.called("rm", "--force", "tcb-ap01") || !r.called("volume", "rm", "tcb-ap01-home") {
		t.Errorf("container and volume should be removed; calls: %v", r.calls)
	}
}

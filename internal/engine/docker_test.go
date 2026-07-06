package engine

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// fakeRunner は docker 呼び出しを記録し、あらかじめ決めた応答を返す。
type fakeRunner struct {
	calls    [][]string
	onOutput func(args []string) (string, error)
	onStream func(args []string) (io.ReadWriteCloser, error)
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
	if f.onStream != nil {
		return f.onStream(args)
	}
	return nopStream{}, nil
}

// nopStream はテスト用の何もしない双方向ストリーム。
type nopStream struct{}

func (nopStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (nopStream) Write(p []byte) (int, error) { return len(p), nil }
func (nopStream) Close() error                { return nil }

func TestContainerStateNotFound(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "", errors.New("docker container: Error: No such container: tcb-ap01")
	}}
	c := NewDockerWithRunner(r)
	state, err := c.ContainerState("tcb-ap01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "" {
		t.Errorf("state = %q, want empty", state)
	}
}

func TestContainerStateRunning(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "running\n", nil
	}}
	c := NewDockerWithRunner(r)
	state, err := c.ContainerState("tcb-ap01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "running" {
		t.Errorf("state = %q, want running", state)
	}
}

func TestVolumeSiteLabelNotFound(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "", errors.New("docker volume: Error response from daemon: get x: no such volume")
	}}
	c := NewDockerWithRunner(r)
	_, exists, err := c.VolumeSiteLabel("x", "tcb.site")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
}

func TestRunDetachedArgs(t *testing.T) {
	r := &fakeRunner{}
	c := NewDockerWithRunner(r)
	err := c.RunDetached(RunOpts{
		Name:     "tcb-ap01",
		Hostname: "ap01.tcb",
		Image:    "tcb:latest",
		Labels:   map[string]string{"tcb.site": "ap01", "tcb.workdir": "/home/x/tcb/ap01"},
		Env:      map[string]string{"TCB_SITE": "ap01"},
		Volumes:  []string{"tcb-ap01-home:/home/tcb", "/home/x/tcb/ap01:/work"},
		Workdir:  "/work",
		Command:  []string{"sleep", "infinity"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"run", "--detach", "--init", "--name", "tcb-ap01",
		"--hostname", "ap01.tcb",
		"--label", "tcb.site=ap01",
		"--label", "tcb.workdir=/home/x/tcb/ap01",
		"--env", "TCB_SITE=ap01",
		"--volume", "tcb-ap01-home:/home/tcb",
		"--volume", "/home/x/tcb/ap01:/work",
		"--workdir", "/work",
		"tcb:latest", "sleep", "infinity",
	}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("docker args:\n got %v\nwant %v", r.calls, want)
	}
}

func TestListBoxes(t *testing.T) {
	out := strings.Join([]string{
		"tcb-ap01\tap01\trunning\t/Users/x/tcb/ap01\t2 hours ago",
		"tcb-us01\tus01\texited\t/Users/x/tcb/us01\t3 days ago",
	}, "\n") + "\n"
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return out, nil
	}}
	c := NewDockerWithRunner(r)
	boxes, err := c.ListBoxes("tcb.site", "tcb.workdir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(boxes) != 2 {
		t.Fatalf("len(boxes) = %d, want 2", len(boxes))
	}
	want := Box{Name: "tcb-ap01", Site: "ap01", State: "running",
		Workdir: "/Users/x/tcb/ap01", RunningFor: "2 hours ago"}
	if boxes[0] != want {
		t.Errorf("boxes[0] = %+v, want %+v", boxes[0], want)
	}
}

func TestDockerBuildNoCacheAndArgs(t *testing.T) {
	r := &fakeRunner{}
	c := NewDockerWithRunner(r)
	err := c.Build("/ctx", "tcb:latest", BuildOpts{
		NoCache:   true,
		BuildArgs: map[string]string{"TDX_VERSION": "2026.6.5"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"build", "-t", "tcb:latest", "--no-cache",
		"--build-arg", "TDX_VERSION=2026.6.5", "/ctx"}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("args = %v, want %v", r.calls, want)
	}
}

func TestDockerBuildCustomDockerfile(t *testing.T) {
	r := &fakeRunner{}
	c := NewDockerWithRunner(r)
	if err := c.Build("/ctx", "tcb:latest", BuildOpts{Dockerfile: "/cfg/Dockerfile"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"build", "-t", "tcb:latest", "-f", "/cfg/Dockerfile", "/ctx"}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("args = %v, want %v", r.calls, want)
	}
}

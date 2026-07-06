package engine

import (
	"errors"
	"reflect"
	"testing"
)

// 実機の container CLI 0.4.1 の出力から採取した JSON。
const appleInspectJSON = `[{"status":"running","networks":[{"gateway":"192.168.64.1","hostname":"tcb-ap01","network":"default","address":"192.168.64.2/24"}],"configuration":{"labels":{"tcb.site":"ap01","tcb.workdir":"/Users/x/tcb/ap01"},"id":"tcb-ap01"}}]`

const appleVolumeInspectJSON = `[{"driver":"local","name":"tcb-ap01-home","format":"ext4","options":{},"labels":{"tcb.site":"ap01"},"createdAt":804865446.7,"source":"/tmp/volume.img"}]`

func TestAppleContainerStateNotFound(t *testing.T) {
	// 存在しないコンテナの inspect は空配列を返す(exit 0)
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "[]", nil
	}}
	a := NewAppleWithRunner(r)
	state, err := a.ContainerState("tcb-nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "" {
		t.Errorf("state = %q, want empty", state)
	}
}

func TestAppleContainerStateAndLabel(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleInspectJSON, nil
	}}
	a := NewAppleWithRunner(r)
	state, err := a.ContainerState("tcb-ap01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "running" {
		t.Errorf("state = %q, want running", state)
	}
	label, err := a.ContainerLabel("tcb-ap01", "tcb.workdir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if label != "/Users/x/tcb/ap01" {
		t.Errorf("label = %q, want /Users/x/tcb/ap01", label)
	}
}

func TestAppleVolumeSiteLabel(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleVolumeInspectJSON, nil
	}}
	a := NewAppleWithRunner(r)
	label, exists, err := a.VolumeSiteLabel("tcb-ap01-home", "tcb.site")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists || label != "ap01" {
		t.Errorf("label, exists = %q, %v; want ap01, true", label, exists)
	}
}

func TestAppleVolumeSiteLabelNotFound(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "", errors.New(`container volume: invalidArgument: "Volume 'x' not found"`)
	}}
	a := NewAppleWithRunner(r)
	_, exists, err := a.VolumeSiteLabel("x", "tcb.site")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
}

func TestAppleVolumeCreateNameBeforeOptions(t *testing.T) {
	// container volume create は名前がオプションより先(0.4.1 の実挙動)
	r := &fakeRunner{}
	a := NewAppleWithRunner(r)
	if err := a.VolumeCreate("tcb-ap01-home", map[string]string{"tcb.site": "ap01"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"volume", "create", "tcb-ap01-home", "--label", "tcb.site=ap01"}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("args = %v, want %v", r.calls, want)
	}
}

func TestAppleRunDetachedArgs(t *testing.T) {
	r := &fakeRunner{}
	a := NewAppleWithRunner(r)
	err := a.RunDetached(RunOpts{
		Name:     "tcb-ap01",
		Hostname: "ap01.tcb", // Apple container では無視される
		Image:    "tcb:latest",
		Labels:   map[string]string{"tcb.site": "ap01"},
		Env:      map[string]string{"TCB_SITE": "ap01"},
		Volumes:  []string{"tcb-ap01-home:/home/tcb"},
		Workdir:  "/work",
		Command:  []string{"sleep", "infinity"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"run", "--detach", "--name", "tcb-ap01",
		"--label", "tcb.site=ap01",
		"--env", "TCB_SITE=ap01",
		"--volume", "tcb-ap01-home:/home/tcb",
		"--workdir", "/work",
		"tcb:latest", "sleep", "infinity",
	}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("args:\n got %v\nwant %v", r.calls, want)
	}
}

func TestAppleListBoxesFiltersByLabel(t *testing.T) {
	list := `[
	  {"status":"running","configuration":{"id":"tcb-ap01","labels":{"tcb.site":"ap01","tcb.workdir":"/w"}}},
	  {"status":"stopped","configuration":{"id":"unrelated","labels":{}}}
	]`
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return list, nil
	}}
	a := NewAppleWithRunner(r)
	boxes, err := a.ListBoxes("tcb.site", "tcb.workdir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(boxes) != 1 {
		t.Fatalf("len(boxes) = %d, want 1", len(boxes))
	}
	want := Box{Name: "tcb-ap01", Site: "ap01", State: "running", Workdir: "/w", RunningFor: "-"}
	if boxes[0] != want {
		t.Errorf("boxes[0] = %+v, want %+v", boxes[0], want)
	}
}

func TestAppleRemoveUsesDeleteForce(t *testing.T) {
	r := &fakeRunner{}
	a := NewAppleWithRunner(r)
	if err := a.Remove("tcb-ap01"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"delete", "--force", "tcb-ap01"}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("args = %v, want %v", r.calls, want)
	}
}

// container 1.0 系のスキーマ: status はオブジェクト、volume の name/labels は
// configuration 配下にネストされる。
const appleInspectJSONv1 = `[{"status":{"state":"running","networks":[],"startedDate":804900000.0},"configuration":{"labels":{"tcb.site":"ap01","tcb.workdir":"/w"},"id":"tcb-ap01"}}]`

const appleVolumeInspectJSONv1 = `[{"id":"tcb-ap01-home","configuration":{"name":"tcb-ap01-home","labels":{"tcb.site":"ap01"},"creationDate":804900000.0}}]`

func TestAppleContainerStateV1Schema(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleInspectJSONv1, nil
	}}
	a := NewAppleWithRunner(r)
	state, err := a.ContainerState("tcb-ap01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "running" {
		t.Errorf("state = %q, want running", state)
	}
}

func TestAppleListBoxesV1Schema(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleInspectJSONv1, nil
	}}
	a := NewAppleWithRunner(r)
	boxes, err := a.ListBoxes("tcb.site", "tcb.workdir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Box{Name: "tcb-ap01", Site: "ap01", State: "running", Workdir: "/w", RunningFor: "-"}
	if len(boxes) != 1 || boxes[0] != want {
		t.Errorf("boxes = %+v, want [%+v]", boxes, want)
	}
}

func TestAppleVolumeSiteLabelV1Schema(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return appleVolumeInspectJSONv1, nil
	}}
	a := NewAppleWithRunner(r)
	label, exists, err := a.VolumeSiteLabel("tcb-ap01-home", "tcb.site")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists || label != "ap01" {
		t.Errorf("label, exists = %q, %v; want ap01, true", label, exists)
	}
}

func TestAppleVolumeNotFoundCamelCase(t *testing.T) {
	r := &fakeRunner{onOutput: func(args []string) (string, error) {
		return "", errors.New(`container volume: Error: notFound: "volume tcb-x-home"`)
	}}
	a := NewAppleWithRunner(r)
	_, exists, err := a.VolumeSiteLabel("tcb-x-home", "tcb.site")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
}

package engine

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Apple は Apple container CLI(https://github.com/apple/container)バックエンド。
// docker と違い inspect 系は Go テンプレートを持たないため JSON をパースする。
// ホスト名は常にコンテナ名になる(--hostname 相当のフラグはない)。
type Apple struct {
	r Runner
}

// NewApple は container CLI を実行するバックエンドを返す。
func NewApple() *Apple { return &Apple{r: &execRunner{bin: "container"}} }

// NewAppleWithRunner はテスト用に Runner を差し替えたバックエンドを返す。
func NewAppleWithRunner(r Runner) *Apple { return &Apple{r: r} }

func (a *Apple) Name() string { return "apple" }

func (a *Apple) Available() error {
	if _, err := exec.LookPath("container"); err != nil {
		return fmt.Errorf("container CLI not found in PATH")
	}
	cmd := exec.Command("container", "system", "status")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("container services not running (start with 'container system start'): %s",
			strings.TrimSpace(string(out)))
	}
	return nil
}

// appleContainer は container inspect / list --format json の1エントリ。
type appleContainer struct {
	Status        string `json:"status"`
	Configuration struct {
		ID     string            `json:"id"`
		Labels map[string]string `json:"labels"`
	} `json:"configuration"`
}

// appleVolume は container volume inspect / ls --format json の1エントリ。
type appleVolume struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

func (a *Apple) inspectContainer(name string) (*appleContainer, error) {
	out, err := a.r.Output("inspect", name)
	if err != nil {
		return nil, err
	}
	var entries []appleContainer
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parse container inspect output: %w", err)
	}
	// 存在しないコンテナは空配列で返る
	if len(entries) == 0 {
		return nil, nil
	}
	return &entries[0], nil
}

func (a *Apple) ImageExists(tag string) bool {
	_, err := a.r.Output("images", "inspect", tag)
	return err == nil
}

func (a *Apple) Build(ctxDir, tag string, o BuildOpts) error {
	// container build は -f を省略すると CWD 基準で Dockerfile を探すため常に明示する
	dockerfile := o.Dockerfile
	if dockerfile == "" {
		dockerfile = filepath.Join(ctxDir, "Dockerfile")
	}
	args := []string{"build", "-t", tag, "-f", dockerfile}
	if o.NoCache {
		args = append(args, "--no-cache")
	}
	for _, k := range sortedKeys(o.BuildArgs) {
		args = append(args, "--build-arg", k+"="+o.BuildArgs[k])
	}
	args = append(args, ctxDir)
	return a.r.Interactive(args...)
}

func (a *Apple) ContainerState(name string) (string, error) {
	entry, err := a.inspectContainer(name)
	if err != nil || entry == nil {
		return "", err
	}
	return entry.Status, nil
}

func (a *Apple) ContainerLabel(name, key string) (string, error) {
	entry, err := a.inspectContainer(name)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", fmt.Errorf("no such container %q", name)
	}
	return entry.Configuration.Labels[key], nil
}

func (a *Apple) RunDetached(o RunOpts) error {
	// Apple container に --hostname / --init はない。ホスト名はコンテナ名になり、
	// コンテナは軽量 VM 内で vminitd が init を務めるため孤児プロセスも回収される。
	args := []string{"run", "--detach", "--name", o.Name}
	for _, k := range sortedKeys(o.Labels) {
		args = append(args, "--label", k+"="+o.Labels[k])
	}
	for _, k := range sortedKeys(o.Env) {
		args = append(args, "--env", k+"="+o.Env[k])
	}
	for _, v := range o.Volumes {
		args = append(args, "--volume", v)
	}
	if o.Workdir != "" {
		args = append(args, "--workdir", o.Workdir)
	}
	args = append(args, o.Image)
	args = append(args, o.Command...)
	_, err := a.r.Output(args...)
	return err
}

func (a *Apple) Start(name string) error {
	_, err := a.r.Output("start", name)
	return err
}

func (a *Apple) ExecInteractive(o ExecOpts) error {
	args := []string{"exec", "--interactive", "--tty"}
	if o.User != "" {
		args = append(args, "--user", o.User)
	}
	for _, k := range sortedKeys(o.Env) {
		args = append(args, "--env", k+"="+o.Env[k])
	}
	if o.Workdir != "" {
		args = append(args, "--workdir", o.Workdir)
	}
	args = append(args, o.Name)
	args = append(args, o.Command...)
	return a.r.Interactive(args...)
}

func (a *Apple) Stop(name string) error {
	_, err := a.r.Output("stop", name)
	return err
}

func (a *Apple) Remove(name string) error {
	_, err := a.r.Output("delete", "--force", name)
	return err
}

func (a *Apple) VolumeSiteLabel(name, key string) (string, bool, error) {
	out, err := a.r.Output("volume", "inspect", name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return "", false, nil
		}
		return "", false, err
	}
	var entries []appleVolume
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return "", false, fmt.Errorf("parse volume inspect output: %w", err)
	}
	if len(entries) == 0 {
		return "", false, nil
	}
	return entries[0].Labels[key], true, nil
}

func (a *Apple) VolumeCreate(name string, labels map[string]string) error {
	// container volume create は名前がオプションより先でないと解釈されない
	args := []string{"volume", "create", name}
	for _, k := range sortedKeys(labels) {
		args = append(args, "--label", k+"="+labels[k])
	}
	_, err := a.r.Output(args...)
	return err
}

func (a *Apple) VolumeRemove(name string) error {
	_, err := a.r.Output("volume", "delete", name)
	return err
}

func (a *Apple) ListBoxes(siteLabel, workdirLabel string) ([]Box, error) {
	// container list にラベルフィルタはないため JSON を取得してこちらで絞る
	out, err := a.r.Output("list", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	var entries []appleContainer
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parse container list output: %w", err)
	}
	var boxes []Box
	for _, e := range entries {
		site, ok := e.Configuration.Labels[siteLabel]
		if !ok {
			continue
		}
		boxes = append(boxes, Box{
			Name:       e.Configuration.ID,
			Site:       site,
			State:      e.Status,
			Workdir:    e.Configuration.Labels[workdirLabel],
			RunningFor: "-", // Apple container は起動時刻を公開しない
		})
	}
	return boxes, nil
}

func (a *Apple) ListVolumes(labelKey string) ([]string, error) {
	out, err := a.r.Output("volume", "ls", "--format", "json")
	if err != nil {
		return nil, err
	}
	var entries []appleVolume
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parse volume list output: %w", err)
	}
	var names []string
	for _, e := range entries {
		if _, ok := e.Labels[labelKey]; ok {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

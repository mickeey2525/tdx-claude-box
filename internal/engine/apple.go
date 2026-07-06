package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Apple は Apple container CLI(https://github.com/apple/container)バックエンド。
// docker と違い inspect 系は Go テンプレートを持たないため JSON をパースする。
// ホスト名は常にコンテナ名になる(--hostname 相当のフラグはない)。
type Apple struct {
	r Runner
	// socat プリフライトはコンテナごとに1回で足りる(1セッション=1コンテナ)。
	socatOnce sync.Once
	socatErr  error
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

// appleNetwork は inspect の status.networks 配列の1エントリ(1.0.0 実機で確認)。
type appleNetwork struct {
	IPv4Gateway string `json:"ipv4Gateway"`
}

// appleStatus は inspect / list の status オブジェクト(1.0 系)。
// 0.4 系の文字列形式はサポートしない。
type appleStatus struct {
	State    string         `json:"state"`
	Networks []appleNetwork `json:"networks"`
}

// appleContainer は container inspect / list --format json の1エントリ。
type appleContainer struct {
	Status        appleStatus `json:"status"`
	Configuration struct {
		ID     string            `json:"id"`
		Labels map[string]string `json:"labels"`
	} `json:"configuration"`
}

// appleVolume は container volume inspect / ls --format json の1エントリ
// (1.0 系。name/labels は configuration 配下)。
type appleVolume struct {
	Configuration struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"configuration"`
}

func (a *Apple) inspectContainer(name string) (*appleContainer, error) {
	out, err := a.r.Output("inspect", name)
	if err != nil {
		// 1.0 系は存在しないコンテナで notFound エラーを返す
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []appleContainer
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parse container inspect output: %w", err)
	}
	// 念のため空配列(exit 0)も不在として扱う
	if len(entries) == 0 {
		return nil, nil
	}
	return &entries[0], nil
}

// isNotFound はリソース不在エラーか判定する。文言はバージョンで揺れる
// ("not found" / "notFound")ため両方見る。
func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "notFound")
}

func (a *Apple) ImageExists(tag string) bool {
	_, err := a.r.Output("image", "inspect", tag)
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
	return entry.Status.State, nil
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
		if isNotFound(err) {
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
	return entries[0].Configuration.Labels[key], true, nil
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
			State:      e.Status.State,
			Workdir:    e.Configuration.Labels[workdirLabel],
			RunningFor: "-", // Apple container は起動時刻を公開しない
		})
	}
	return boxes, nil
}

// containerGateway は inspect から vmnet ゲートウェイ IP を得る。
func (a *Apple) containerGateway(name string) (string, error) {
	entry, err := a.inspectContainer(name)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", fmt.Errorf("no such container %q", name)
	}
	nets := entry.Status.Networks
	if len(nets) == 0 || nets[0].IPv4Gateway == "" {
		return "", fmt.Errorf("container %q has no network info (is it running?)", name)
	}
	return nets[0].IPv4Gateway, nil
}

// BridgeAddrs: Apple container の VM からホストの loopback には届かないため、
// vmnet のゲートウェイ IP(ホスト側インターフェース)にバインドし、
// コンテナも同じ IP へ接続する。
func (a *Apple) BridgeAddrs(name string) (string, string, error) {
	gw, err := a.containerGateway(name)
	return gw, gw, err
}

// DialContainerPort: ホストからコンテナ IP へ直接 TCP も張れるが、それでは
// コンテナ内で 127.0.0.1 にバインドしたサーバー(OAuth コールバックの通例)に
// 届かない。docker バックエンドと同様に exec + socat でコンテナの netns 内
// から loopback へ接続する。
func (a *Apple) DialContainerPort(name string, port int) (io.ReadWriteCloser, error) {
	// socat 追加前に作られたイメージへの分かりやすい導線を出す
	// (which は slim イメージに無いことがあるため socat 自身を叩く)
	a.socatOnce.Do(func() {
		if _, err := a.r.Output("exec", name, "socat", "-V"); err != nil {
			a.socatErr = fmt.Errorf(
				"socat not found in the box image (rebuild it with 'tcb run <box> --rebuild')")
		}
	})
	if a.socatErr != nil {
		return nil, a.socatErr
	}
	return a.r.Stream("exec", "--interactive", name,
		"socat", "STDIO", fmt.Sprintf("TCP:127.0.0.1:%d", port))
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
		if _, ok := e.Configuration.Labels[labelKey]; ok {
			names = append(names, e.Configuration.Name)
		}
	}
	return names, nil
}

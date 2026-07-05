package engine

import (
	"fmt"
	"os/exec"
	"strings"
)

// Docker は docker CLI バックエンド。SDK 依存を持たず子プロセスで docker を叩く。
type Docker struct {
	r Runner
}

// NewDocker は docker CLI を実行するバックエンドを返す。
func NewDocker() *Docker { return &Docker{r: &execRunner{bin: "docker"}} }

// NewDockerWithRunner はテスト用に Runner を差し替えたバックエンドを返す。
func NewDockerWithRunner(r Runner) *Docker { return &Docker{r: r} }

func (d *Docker) Name() string { return "docker" }

func (d *Docker) Available() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker CLI not found in PATH")
	}
	cmd := exec.Command("docker", "info", "--format", "{{.ServerVersion}}")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker daemon not reachable: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *Docker) ImageExists(tag string) bool {
	_, err := d.r.Output("image", "inspect", "--format", "{{.Id}}", tag)
	return err == nil
}

func (d *Docker) Build(ctxDir, tag string, o BuildOpts) error {
	args := []string{"build", "-t", tag}
	if o.Dockerfile != "" {
		args = append(args, "-f", o.Dockerfile)
	}
	if o.NoCache {
		args = append(args, "--no-cache")
	}
	for _, k := range sortedKeys(o.BuildArgs) {
		args = append(args, "--build-arg", k+"="+o.BuildArgs[k])
	}
	args = append(args, ctxDir)
	return d.r.Interactive(args...)
}

func (d *Docker) ContainerState(name string) (string, error) {
	out, err := d.r.Output("container", "inspect", "--format", "{{.State.Status}}", name)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (d *Docker) ContainerLabel(name, key string) (string, error) {
	out, err := d.r.Output("container", "inspect",
		"--format", fmt.Sprintf(`{{index .Config.Labels %q}}`, key), name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (d *Docker) RunDetached(o RunOpts) error {
	args := []string{"run", "--detach", "--init", "--name", o.Name}
	if o.Hostname != "" {
		args = append(args, "--hostname", o.Hostname)
	}
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
	_, err := d.r.Output(args...)
	return err
}

func (d *Docker) Start(name string) error {
	_, err := d.r.Output("start", name)
	return err
}

func (d *Docker) ExecInteractive(o ExecOpts) error {
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
	return d.r.Interactive(args...)
}

func (d *Docker) Stop(name string) error {
	_, err := d.r.Output("stop", name)
	return err
}

func (d *Docker) Remove(name string) error {
	_, err := d.r.Output("rm", "--force", name)
	return err
}

func (d *Docker) VolumeSiteLabel(name, key string) (string, bool, error) {
	out, err := d.r.Output("volume", "inspect",
		"--format", fmt.Sprintf(`{{index .Labels %q}}`, key), name)
	if err != nil {
		if strings.Contains(err.Error(), "no such volume") ||
			strings.Contains(err.Error(), "No such volume") {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

func (d *Docker) VolumeCreate(name string, labels map[string]string) error {
	args := []string{"volume", "create"}
	for _, k := range sortedKeys(labels) {
		args = append(args, "--label", k+"="+labels[k])
	}
	args = append(args, name)
	_, err := d.r.Output(args...)
	return err
}

func (d *Docker) VolumeRemove(name string) error {
	_, err := d.r.Output("volume", "rm", name)
	return err
}

func (d *Docker) ListBoxes(siteLabel, workdirLabel string) ([]Box, error) {
	format := fmt.Sprintf(`{{.Names}}\t{{.Label %q}}\t{{.State}}\t{{.Label %q}}\t{{.RunningFor}}`,
		siteLabel, workdirLabel)
	out, err := d.r.Output("ps", "--all", "--filter", "label="+siteLabel, "--format", format)
	if err != nil {
		return nil, err
	}
	var boxes []Box
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		boxes = append(boxes, Box{Name: f[0], Site: f[1], State: f[2], Workdir: f[3], RunningFor: f[4]})
	}
	return boxes, nil
}

func (d *Docker) ListVolumes(labelKey string) ([]string, error) {
	out, err := d.r.Output("volume", "ls", "--filter", "label="+labelKey, "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

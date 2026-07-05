package commands

import (
	"fmt"
	"io"

	"github.com/mickeey2525/tdx-claude-box/internal/config"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

// Doctor はバックエンド・イメージ・box・ボリュームの状態を診断する。
// backendName は --backend / TCB_BACKEND の値(空なら自動検出)。
func Doctor(backendName string, w io.Writer) error {
	for _, b := range engine.Backends() {
		if err := b.Available(); err != nil {
			fmt.Fprintf(w, "- backend %s: %v\n", b.Name(), err)
		} else {
			fmt.Fprintf(w, "✓ backend %s: available\n", b.Name())
		}
	}

	e, err := engine.Select(backendName)
	if err != nil {
		fmt.Fprintln(w, "✗ no usable backend")
		return fmt.Errorf("doctor found problems")
	}
	fmt.Fprintf(w, "✓ using backend: %s\n", e.Name())

	ok := true

	if e.ImageExists(config.ImageTag) {
		fmt.Fprintf(w, "✓ image %s: present\n", config.ImageTag)
	} else {
		fmt.Fprintf(w, "- image %s: not built yet (built automatically on first 'tcb run')\n", config.ImageTag)
	}

	boxes, err := e.ListBoxes(config.LabelSite, config.LabelWorkdir)
	if err != nil {
		return err
	}
	if len(boxes) == 0 {
		fmt.Fprintln(w, "- boxes: none")
	}
	for _, b := range boxes {
		expected := config.ContainerName(b.Site)
		if b.Name != expected {
			ok = false
			fmt.Fprintf(w, "✗ box %s: name does not match site label %q\n", b.Name, b.Site)
			continue
		}
		fmt.Fprintf(w, "✓ box %s: %s (workdir: %s)\n", b.Name, b.State, b.Workdir)
	}

	volumes, err := e.ListVolumes(config.LabelSite)
	if err != nil {
		return err
	}
	for _, v := range volumes {
		label, _, err := e.VolumeSiteLabel(v, config.LabelSite)
		if err != nil {
			return err
		}
		expected := config.VolumeName(label)
		if v != expected {
			ok = false
			fmt.Fprintf(w, "✗ volume %s: name does not match site label %q (expected %s)\n", v, label, expected)
			continue
		}
		orphan := ""
		if !hasBoxForSite(boxes, label) {
			orphan = " (no container; will be reused by 'tcb run " + label + "')"
		}
		fmt.Fprintf(w, "✓ volume %s: site %s%s\n", v, label, orphan)
	}

	if !ok {
		return fmt.Errorf("doctor found problems")
	}
	fmt.Fprintln(w, "all good")
	return nil
}

func hasBoxForSite(boxes []engine.Box, site string) bool {
	for _, b := range boxes {
		if b.Site == site {
			return true
		}
	}
	return false
}

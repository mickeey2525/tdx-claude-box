package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    runOptions
		wantErr bool
	}{
		{
			name: "site only",
			args: []string{"ap01"},
			want: runOptions{Site: "ap01"},
		},
		{
			name: "flags after site",
			args: []string{"ap01", "--dir", "/tmp/x", "--rebuild"},
			want: runOptions{Site: "ap01", Dir: "/tmp/x", Rebuild: true},
		},
		{
			name: "flags before site",
			args: []string{"--rebuild", "--dir=/tmp/x", "ap01"},
			want: runOptions{Site: "ap01", Dir: "/tmp/x", Rebuild: true},
		},
		{
			name: "passthrough after double dash",
			args: []string{"ap01", "--", "--model", "opus"},
			want: runOptions{Site: "ap01", Passthrough: []string{"--model", "opus"}},
		},
		{
			name: "rebuild-looking flag in passthrough stays untouched",
			args: []string{"ap01", "--", "--rebuild"},
			want: runOptions{Site: "ap01", Passthrough: []string{"--rebuild"}},
		},
		{name: "no site", args: []string{}, wantErr: true},
		{name: "no site before double dash", args: []string{"--", "x"}, wantErr: true},
		{name: "unknown flag", args: []string{"ap01", "--bogus"}, wantErr: true},
		{name: "dir without value", args: []string{"ap01", "--dir"}, wantErr: true},
		{name: "two sites", args: []string{"ap01", "us01"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRunArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseRunArgs(%v) = %+v, want error", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRunArgs(%v) error: %v", tt.args, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseRunArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseRunArgsTDSite(t *testing.T) {
	got, err := parseRunArgs([]string{"us01-7060", "--site", "us01"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := runOptions{Site: "us01-7060", TDSite: "us01"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	got, err = parseRunArgs([]string{"--site=us01", "us01-7060"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if _, err := parseRunArgs([]string{"us01-7060", "--site"}); err == nil {
		t.Error("expected error for --site without value")
	}
}

func TestCustomDockerfile(t *testing.T) {
	dir := t.TempDir()
	df := filepath.Join(dir, "Dockerfile.custom")
	if err := os.WriteFile(df, []byte("FROM tcb:base\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TCB_DOCKERFILE", df)
	got, err := customDockerfile()
	if err != nil || got != df {
		t.Errorf("customDockerfile() = %q, %v; want %q, nil", got, err, df)
	}

	t.Setenv("TCB_DOCKERFILE", "none")
	got, err = customDockerfile()
	if err != nil || got != "" {
		t.Errorf("customDockerfile() with none = %q, %v; want empty, nil", got, err)
	}

	t.Setenv("TCB_DOCKERFILE", filepath.Join(dir, "missing"))
	if _, err := customDockerfile(); err == nil {
		t.Error("expected error for missing TCB_DOCKERFILE path")
	}
}

func TestSyncProjectSettingsCopiesSettingsJSON(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".claude", "settings.json"), []byte("{\"permissions\":{}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".claude", "settings.local.json"), []byte("{\"site\":\"host\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncProjectSettings(src, dst); err != nil {
		t.Fatalf("syncProjectSettings() error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json was not copied: %v", err)
	}
	if string(got) != "{\"permissions\":{}}\n" {
		t.Errorf("settings.json = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Errorf("settings.local.json must not be copied; stat err = %v", err)
	}
}

func TestSyncProjectSettingsNoopWhenSameDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncProjectSettings(dir, dir); err != nil {
		t.Fatalf("syncProjectSettings() error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}\n" {
		t.Errorf("settings.json changed to %q", got)
	}
}

func TestSyncProjectSettingsRemovesPreviousManagedSettings(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	dstSettings := filepath.Join(dst, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(dstSettings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstSettings, []byte("{\"old\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectSettingsMarker(dstSettings), []byte(filepath.Join(src, ".claude", "settings.json")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncProjectSettings(src, dst); err != nil {
		t.Fatalf("syncProjectSettings() error: %v", err)
	}

	if _, err := os.Stat(dstSettings); !os.IsNotExist(err) {
		t.Errorf("managed settings.json should be removed; stat err = %v", err)
	}
	if _, err := os.Stat(projectSettingsMarker(dstSettings)); !os.IsNotExist(err) {
		t.Errorf("managed marker should be removed; stat err = %v", err)
	}
}

func TestSyncProjectSettingsKeepsUnmanagedSettingsWhenSourceMissing(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	dstSettings := filepath.Join(dst, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(dstSettings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstSettings, []byte("{\"manual\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncProjectSettings(src, dst); err != nil {
		t.Fatalf("syncProjectSettings() error: %v", err)
	}

	got, err := os.ReadFile(dstSettings)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\"manual\":true}\n" {
		t.Errorf("unmanaged settings.json changed to %q", got)
	}
}

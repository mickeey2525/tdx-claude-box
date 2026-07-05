package commands

import (
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

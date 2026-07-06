package commands

import (
	"fmt"
	"io"
	"runtime/debug"
)

// Version はバージョン情報を表示する。
func Version(w io.Writer) error {
	fmt.Fprintln(w, versionString())
	return nil
}

// versionString はビルド情報からバージョン文字列を組み立てる。
// go install <module>@latest 経由なら疑似バージョン、リポジトリ内での
// go build/install なら "(devel)" + VCS リビジョンになる。
func versionString() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "tcb (unknown build)"
	}
	s := "tcb " + bi.Main.Version
	var rev, modified string
	for _, kv := range bi.Settings {
		switch kv.Key {
		case "vcs.revision":
			rev = kv.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
		case "vcs.modified":
			if kv.Value == "true" {
				modified = " (modified)"
			}
		}
	}
	// 疑似バージョンにはリビジョンが含まれるので、(devel) のときだけ補う
	if rev != "" && bi.Main.Version == "(devel)" {
		s += " " + rev
	}
	return s + modified
}

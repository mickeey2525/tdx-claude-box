package engine

import (
	"fmt"
	"strings"
)

// Backends は選択可能なバックエンドを優先順で返す。
func Backends() []Engine {
	return []Engine{NewDocker(), NewApple()}
}

// Select はバックエンドを解決する。
// name には "docker" / "apple"(別名 "container")、または空文字列(自動検出)を渡す。
// 自動検出は docker → apple の順で最初に使用可能なものを選ぶ。
func Select(name string) (Engine, error) {
	switch strings.ToLower(name) {
	case "docker":
		e := NewDocker()
		if err := e.Available(); err != nil {
			return nil, err
		}
		return e, nil
	case "apple", "container":
		e := NewApple()
		if err := e.Available(); err != nil {
			return nil, err
		}
		return e, nil
	case "":
		var reasons []string
		for _, e := range Backends() {
			if err := e.Available(); err == nil {
				return e, nil
			} else {
				reasons = append(reasons, fmt.Sprintf("%s: %v", e.Name(), err))
			}
		}
		return nil, fmt.Errorf("no container backend available\n  %s", strings.Join(reasons, "\n  "))
	default:
		return nil, fmt.Errorf("unknown backend %q (use docker or apple)", name)
	}
}

// Package cli はサブコマンドのディスパッチを行う。
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mickeey2525/tdx-claude-box/internal/commands"
	"github.com/mickeey2525/tdx-claude-box/internal/engine"
)

const usage = `tcb - run tdx claude in per-site isolated containers

Usage:
  tcb [--backend docker|apple] <command> [args...]

Commands:
  run <box> [--site <td-site>] [--dir <path>] [--rebuild] [-- <tdx claude args...>]
        Start (or attach to) the box and enter tdx claude.
        <box> is the isolation unit name; --site sets the actual TD site when
        it differs (e.g. tcb run us01-7060 --site us01 for account td7060).
  ls
        List boxes.
  shell <site>
        Open a bash shell in the box (for debugging / tdx auth setup).
  stop <site>
        Stop the box container (keeps the home volume).
  rm <site> [--volumes]
        Remove the box container. --volumes also deletes credentials.
  doctor
        Diagnose backends, image, boxes and volumes.

Backend selection (default: auto-detect, docker first):
  --backend docker|apple    or environment variable TCB_BACKEND
`

// Run は引数を解釈してサブコマンドを実行し、プロセスの終了コードを返す。
func Run(args []string) int {
	backend := os.Getenv("TCB_BACKEND")
	for len(args) > 0 {
		switch {
		case args[0] == "--backend" && len(args) > 1:
			backend = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--backend="):
			backend = strings.TrimPrefix(args[0], "--backend=")
			args = args[1:]
		default:
			goto parsed
		}
	}
parsed:

	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	cmd, rest := args[0], args[1:]

	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Print(usage)
		return 0
	}

	var err error
	if cmd == "doctor" {
		// doctor はバックエンド不在でも診断結果を出す
		err = commands.Doctor(backend, os.Stdout)
	} else {
		var e engine.Engine
		e, err = engine.Select(backend)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tcb: %v\n", err)
			return 1
		}
		switch cmd {
		case "run":
			err = commands.Run(e, rest)
		case "ls":
			err = commands.Ls(e, os.Stdout)
		case "shell":
			err = commands.Shell(e, rest)
		case "stop":
			err = commands.Stop(e, rest)
		case "rm":
			err = commands.Rm(e, rest, os.Stdin, os.Stdout)
		default:
			fmt.Fprintf(os.Stderr, "tcb: unknown command %q\n\n%s", cmd, usage)
			return 2
		}
	}

	if err != nil {
		// 対話セッション(tdx claude / bash)の終了コードはそのまま伝播する
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "tcb: %v\n", err)
		return 1
	}
	return 0
}

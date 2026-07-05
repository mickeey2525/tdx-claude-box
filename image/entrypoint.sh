#!/usr/bin/env bash
# tcb-entry: tcb run <site> が docker exec で呼ぶセッションエントリポイント。
# site マーカー検証 → tdx use site → 初回は API キーを保存 → tdx claude
set -euo pipefail

: "${TCB_SITE:?TCB_SITE is not set (start this container via 'tcb run <site>')}"
td_site="${TCB_TD_SITE:-$TCB_SITE}"

marker="$HOME/.tcb-site"
if [[ -f "$marker" ]]; then
    existing="$(cat "$marker")"
    if [[ "$existing" != "$TCB_SITE" ]]; then
        echo "tcb: this home volume belongs to site '$existing', refusing to start as '$TCB_SITE'" >&2
        echo "tcb: remove the box with 'tcb rm $TCB_SITE --volumes' if you really want to reuse it" >&2
        exit 1
    fi
else
    echo "$TCB_SITE" > "$marker"
fi

# TD site も検証する。ボリューム内の API キーは site 固有なので、
# 同じ box 名のまま別 TD site に切り替えるのは事故のもと。
td_marker="$HOME/.tcb-td-site"
if [[ -f "$td_marker" ]]; then
    existing_td="$(cat "$td_marker")"
    if [[ "$existing_td" != "$td_site" ]]; then
        echo "tcb: this box's volume was set up for TD site '$existing_td', not '$td_site'" >&2
        echo "tcb: credentials are site-specific; 'tcb rm $TCB_SITE --volumes' to start over" >&2
        exit 1
    fi
else
    echo "$td_site" > "$td_marker"
fi

# box 名(TCB_SITE)と TD site(TCB_TD_SITE)は別物にできる。
# 例: box 'us01-7060' が TD site 'us01' を使う(--site us01)。
# コンテナ内 HOME は box 専用なので --default で安全。
tdx use site "$td_site" --default

# 認証: コンテナ内には OS キーチェーン(Secret Service)がないため
# `tdx auth setup` は保存に失敗する(PermissionDenied)。代わりに初回は
# API キーを聞き、box 専用ボリューム内の env ファイルに保存して
# TDX_API_KEY として tdx に渡す。
env_file="$HOME/.config/tdx/.env"
if ! grep -qs '^TDX_API_KEY=' "$env_file"; then
    echo "==> First run for box '$TCB_SITE' (TD site: $td_site)"
    echo "    tdx cannot use an OS keychain inside a container, so tcb stores your"
    echo "    API key in ~/.config/tdx/.env inside this box's private volume."
    echo "    Get one from TD Console > My Settings > API Keys."
    printf "    Paste API key for %s: " "$td_site"
    read -rs api_key
    echo
    # 端末からの貼り付けで紛れ込みうる CR・前後空白を除去
    api_key="$(printf '%s' "$api_key" | tr -d '\r' | xargs)"
    if [[ -z "$api_key" ]]; then
        echo "tcb: no API key entered" >&2
        exit 1
    fi
    echo "    Validating..."
    # tdx auth status は成功時 0 / 失敗時 1 を返す(出力はそのまま見せる)
    if ! TDX_API_KEY="$api_key" tdx auth status; then
        echo "tcb: API key validation failed for TD site '$td_site'; nothing saved" >&2
        exit 1
    fi
    mkdir -p "$HOME/.config/tdx"
    (umask 077 && printf 'TDX_API_KEY=%s\n' "$api_key" >> "$env_file")
    chmod 600 "$env_file"
    echo "    Saved. To redo: 'tcb shell $TCB_SITE' and edit ~/.config/tdx/.env"
fi

set -a
# shellcheck source=/dev/null
source "$env_file"
set +a

exec tdx claude "$@"

#!/usr/bin/env bash
# tcb-entry: tcb run <site> が docker exec で呼ぶセッションエントリポイント。
# site マーカー検証 → 初回 auth 誘導 → tdx use site → tdx claude
set -euo pipefail

: "${TCB_SITE:?TCB_SITE is not set (start this container via 'tcb run <site>')}"

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

# 初回のみ tdx auth setup へ誘導。認証をやり直したいときは
# `tcb shell <site>` で入って `tdx auth setup` を実行し直せる。
auth_sentinel="$HOME/.tcb-auth-ok"
if [[ ! -f "$auth_sentinel" ]]; then
    echo "==> First run for site '$TCB_SITE': running 'tdx auth setup'"
    echo ""
    echo "    IMPORTANT: choose 'Use an API key' when asked how to sign in."
    echo "    Browser SSO cannot complete inside a container (the OAuth callback"
    echo "    never reaches it). Get an API key from TD Console > My Settings > API Keys."
    echo ""
    tdx auth setup
    touch "$auth_sentinel"
fi

# box 名(TCB_SITE)と TD site(TCB_TD_SITE)は別物にできる。
# 例: box 'us01-7060' が TD site 'us01' を使う(--site us01)。
# コンテナ内 HOME は box 専用なので --default で安全。
tdx use site "${TCB_TD_SITE:-$TCB_SITE}" --default

exec tdx claude "$@"

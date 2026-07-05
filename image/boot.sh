#!/usr/bin/env bash
# tcb-boot: コンテナのメインプロセス(root で実行)。
# HOME ボリュームを初期化してから常駐する。セッションは tcb ユーザーで
# `tcb-entry` / bash を exec して同居する。
#
# Docker の名前付きボリュームは初回マウント時にイメージ内容がコピーされるが、
# Apple container のボリュームは root 所有の空 ext4 なので、ここで所有権と
# 初期ファイルを整える(両バックエンドで冪等)。
set -euo pipefail

home=/home/tcb

chown tcb:tcb "$home"

for f in .bashrc .profile; do
    if [[ ! -e "$home/$f" && -e "/etc/skel/$f" ]]; then
        cp "/etc/skel/$f" "$home/$f"
        chown tcb:tcb "$home/$f"
    fi
done

# プロンプトに site 名を表示する
if ! grep -q TCB_SITE "$home/.bashrc" 2>/dev/null; then
    printf '%s\n' 'export PS1="[\[\e[1;35m\]${TCB_SITE:-tcb}\[\e[0m\]] \w \\$ "' >> "$home/.bashrc"
    chown tcb:tcb "$home/.bashrc"
fi

exec sleep infinity

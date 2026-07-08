# Debian バージョンを明示ピンする(無印 -slim は upstream の都合で Debian が
# 変わる。bookworm の glibc 2.36 は mise 等のプレビルドバイナリが要求する
# glibc を満たさない。trixie は glibc 2.41)
FROM node:24-trixie-slim

# 既定は latest。`tcb run --rebuild` は --no-cache でビルドするので最新が入る。
# 固定したい場合は TCB_TDX_VERSION / TCB_CLAUDE_CODE_VERSION 環境変数で上書きする。
ARG TDX_VERSION=latest
ARG CLAUDE_CODE_VERSION=latest

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        jq \
        less \
        make \
        nano \
        openssh-client \
        procps \
        ripgrep \
        socat \
        tree \
        unzip \
        wget \
        zip \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI(公式 apt リポジトリ。arm64/amd64 両対応)
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# 開発ツール一式(latest を焼き込み、更新は `tcb run --rebuild`)。
# インストール先を /usr/local に向けて全ユーザーから使えるようにする。
# uv: Python パッケージ/ランタイム管理(uv python install で Python 本体も入る)
RUN curl -LsSf https://astral.sh/uv/install.sh \
    | env UV_INSTALL_DIR=/usr/local/bin UV_NO_MODIFY_PATH=1 sh
# bun: JS ランタイム(unzip が必要)
RUN curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash
# mise: ランタイムバージョンマネージャ。shell hook は tcb-boot が .bashrc に足す
RUN curl -fsSL https://mise.run | MISE_INSTALL_PATH=/usr/local/bin/mise sh

RUN npm install -g "@treasuredata/tdx@${TDX_VERSION}"

# Claude Code はネイティブバイナリ版(npm 版より速い。node_modules 展開がない)。
# インストール先は $HOME/.local 固定なので /opt/claude に入れて全ユーザーへ
# symlink する。インストーラはカレントディレクトリを走査するため /tmp で実行。
# 更新は box 内の自動更新ではなく `tcb run --rebuild` で行う(バージョン drift 防止)。
RUN cd /tmp \
    && curl -fsSL https://claude.ai/install.sh | HOME=/opt/claude bash -s "${CLAUDE_CODE_VERSION}" \
    && ln -s /opt/claude/.local/bin/claude /usr/local/bin/claude \
    && chmod -R a+rX /opt/claude
ENV DISABLE_AUTOUPDATER=1
# ブラウザを開くツールが $BROWSER を優先する場合も xdg-open シムへ誘導する
ENV BROWSER=xdg-open

# node イメージは uid 1000 を "node" が使っているので tcb は 1001。
# /home/tcb には site ごとのボリュームがマウントされ、tcb-boot が初期化する。
RUN useradd -m -u 1001 -s /bin/bash tcb \
    && mkdir -p /work \
    && chown tcb:tcb /work

# mise で入れたランタイムと uv tool install の行き先を PATH へ。
# Claude Code の Bash ツールは非対話シェル(.bashrc を読まない)なので、
# .bashrc の mise activate だけでは足りず ENV で焼き込む必要がある
ENV PATH="/home/tcb/.local/share/mise/shims:/home/tcb/.local/bin:${PATH}"

COPY boot.sh /usr/local/bin/tcb-boot
COPY entrypoint.sh /usr/local/bin/tcb-entry
# xdg-open シム: URL を tcb の URL ブリッジへ送ってホストのブラウザで開く
# (socat はホスト側 tcb が docker exec 経由でコールバックを中継するのに使う)
COPY xdg-open /usr/local/bin/xdg-open
RUN chmod +x /usr/local/bin/tcb-boot /usr/local/bin/tcb-entry /usr/local/bin/xdg-open

# メインプロセス(tcb-boot)は root で動き、セッションは --user tcb で exec する
WORKDIR /work
CMD ["tcb-boot"]

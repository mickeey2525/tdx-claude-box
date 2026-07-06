FROM node:24-slim

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
        openssh-client \
        procps \
        ripgrep \
        socat \
    && rm -rf /var/lib/apt/lists/*

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

COPY boot.sh /usr/local/bin/tcb-boot
COPY entrypoint.sh /usr/local/bin/tcb-entry
# xdg-open シム: URL を tcb の URL ブリッジへ送ってホストのブラウザで開く
# (socat はホスト側 tcb が docker exec 経由でコールバックを中継するのに使う)
COPY xdg-open /usr/local/bin/xdg-open
RUN chmod +x /usr/local/bin/tcb-boot /usr/local/bin/tcb-entry /usr/local/bin/xdg-open

# メインプロセス(tcb-boot)は root で動き、セッションは --user tcb で exec する
WORKDIR /work
CMD ["tcb-boot"]

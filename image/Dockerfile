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
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g \
        "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
        "@treasuredata/tdx@${TDX_VERSION}"

# node イメージは uid 1000 を "node" が使っているので tcb は 1001。
# /home/tcb には site ごとのボリュームがマウントされ、tcb-boot が初期化する。
RUN useradd -m -u 1001 -s /bin/bash tcb \
    && mkdir -p /work \
    && chown tcb:tcb /work

COPY boot.sh /usr/local/bin/tcb-boot
COPY entrypoint.sh /usr/local/bin/tcb-entry
# xdg-open シム: tdx auth setup のブラウザ SSO 選択時のクラッシュ防止
COPY xdg-open /usr/local/bin/xdg-open
RUN chmod +x /usr/local/bin/tcb-boot /usr/local/bin/tcb-entry /usr/local/bin/xdg-open

# メインプロセス(tcb-boot)は root で動き、セッションは --user tcb で exec する
WORKDIR /work
CMD ["tcb-boot"]

# tdx-claude-box (`tcb`)

`tdx claude` を **site(us01 / ap01 など)ごとに隔離された Docker コンテナ**で動かす CLI。

同一マシンで複数リージョンの調査を並行すると、`tdx claude` が生成する
`.claude/settings.local.json` や `~/.claude`・`~/.config/tdx` が競合して
**別リージョンのプロキシに繋がる**事故が起きる。`tcb` は site 単位で
HOME ボリュームと作業ディレクトリを分離し、これを構造的に防ぐ。

## インストール

```sh
go install github.com/mickeey2525/tdx-claude-box/cmd/tcb@latest
```

Dockerfile はバイナリに埋め込まれているため、リポジトリの clone は不要。
必要なのは `tcb` バイナリとコンテナランタイム(Docker または Apple container)のみ。

## バックエンド

Docker と [Apple container](https://github.com/apple/container) の両方に対応。
既定では自動検出(docker 優先)。明示するには:

```sh
tcb --backend apple run ap01     # または --backend docker
export TCB_BACKEND=apple         # 環境変数でも指定可能
```

site の隔離単位はバックエンドごとに独立(docker のボリュームと apple のボリュームは別物)。
片方に作った box はもう片方からは見えないので、基本はどちらかに揃えて使う。

## 使い方

```sh
tcb run ap01              # ap01 用 box を作成して tdx claude に入る(初回は tdx auth setup へ誘導)
tcb run us01              # 別ターミナルで us01 を並行調査(完全分離)
tcb run us01-7060 --site us01   # box 名と TD site を分ける(同じ site の別アカウント調査用)
tcb run ap01 --dir ~/investigations/case-123   # 作業ディレクトリを指定
tcb run ap01 -- --model opus                   # -- 以降は tdx claude へそのまま渡す

tcb ls                    # box 一覧
tcb shell ap01            # box に bash で入る(auth のやり直し等)
tcb stop ap01             # コンテナ停止(認証情報は保持)
tcb rm ap01               # box を削除(警告と確認の後、認証情報を含むボリュームも削除)
tcb rm ap01 --keep-volume # コンテナだけ削除して認証情報は残す
tcb doctor                # 環境診断
tcb run ap01 --rebuild    # イメージを再ビルドして tdx / claude-code を最新に更新
```

イメージには `@treasuredata/tdx` と `@anthropic-ai/claude-code` の最新版が入る
(`--rebuild` はキャッシュなしでビルドするので確実に更新される)。
バージョンを固定したい場合は環境変数で指定する:

```sh
TCB_TDX_VERSION=2026.6.5 tcb run ap01 --rebuild
TCB_CLAUDE_CODE_VERSION=2.1.201 tcb run ap01 --rebuild
```

## アップデート

```sh
tcb upgrade                # tcb 本体を go install @latest で更新し、イメージも再ビルド
tcb upgrade --binary-only  # tcb 本体のみ
tcb upgrade --image-only   # イメージのみ(tdx / claude-code を最新化。run --rebuild 相当)
tcb version                # 現在のバージョン確認
```

- バイナリ更新には Go toolchain と、リポジトリ(private の場合)への
  git 認証(`gh auth setup-git` か SSH)が必要
- イメージ更新後も**既存の box は古いイメージのまま**。反映は
  `tcb rm <box>` → `tcb run <box>`(認証を残すなら `--keep-volume`)

## 認証(初回)

初回の `tcb run <site>` は API キーの入力を求める(TD Console → My Settings →
API Keys から取得)。キーは検証したうえでコンテナ内の `~/.config/tdx/.env` に
保存され、以後のセッションでは `TDX_API_KEY` として tdx に渡される。
box 専用ボリュームに永続化されるので、box を消すまで再入力は不要
(`tcb rm` はボリュームごと削除する。認証を残してコンテナだけ作り直すなら
`tcb rm --keep-volume`)。

コンテナ内では `tdx auth setup` は使えない:
- ブラウザ SSO は OAuth コールバックがコンテナ内 localhost に届かず完了しない
- API キー方式も、保存先の OS キーチェーン(Secret Service)がコンテナに
  存在しないため `PermissionDenied` で失敗する

キーを差し替えたいときは `tcb shell <site>` で入って `~/.config/tdx/.env` を
編集する。

## イメージのカスタマイズ(ツールの追加)

`~/.config/tcb/Dockerfile` を置くと、標準イメージ(`tcb:base`)の上に自分の
レイヤーを重ねられる(パスは `TCB_DOCKERFILE` 環境変数でも指定可能。
`TCB_DOCKERFILE=none` で一時的に無効化):

```dockerfile
FROM tcb:base
RUN apt-get update && apt-get install -y --no-install-recommends \
        postgresql-client awscli \
    && rm -rf /var/lib/apt/lists/*
```

- ビルドコンテキストはこの Dockerfile のあるディレクトリ(`COPY` も使える)
- カスタム層は `tcb run` のたびにビルドされる(キャッシュが効くので変更が
  なければ一瞬)。Dockerfile を編集したら次の `tcb run` で新イメージになる
- ただし**既存の box は古いイメージのまま動き続ける**。反映するには
  `tcb rm <box>` して作り直す(認証を残すなら `--keep-volume`)
- ベースの `CMD`(tcb-boot)と `tcb` ユーザーは tcb の動作に必要なので
  上書きしないこと

## 隔離の仕組み

- site ごとに名前付きボリューム `tcb-<site>-home` を `/home/tcb` にマウント
  → `~/.claude`・`~/.config/tdx`・認証情報・プラグインが site ごとに完全分離
- 作業ディレクトリは既定で `~/tcb/<site>`(`--dir` で変更可)を `/work` にマウント
- `tcb run` を実行したディレクトリに `.claude/settings.json` があれば、
  box 側の `/work/.claude/settings.json` へ同期する。`settings.local.json` は
  site ごとに `tdx claude` が生成するため同期しない
- コンテナは 1 site 1 個(`tcb-<site>`)。同じ site の2セッション目は exec で同居
- ガードレール:
  - HOME ボリューム内のマーカーファイル(`~/.tcb-site`)とボリュームラベルで
    別 site での誤使用を拒否
  - ホスト名に site 名が入り(docker: `<site>.tcb` / apple: `tcb-<site>`)、
    シェルプロンプトにも site 名が出る

## 開発

```sh
go test ./...
go build ./cmd/tcb
```

ランタイム呼び出しは `internal/engine` の `Engine` インターフェース
(docker / apple の2実装)に隔離されており、テストはフェイクで実行される
(実ランタイム不要)。

設計の背景・調査メモは [PLAN.md](PLAN.md) を参照。

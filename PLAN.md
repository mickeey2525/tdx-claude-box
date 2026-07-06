# tdx-claude-box 実装計画

作成日: 2026-07-04
コマンド名: `tcb`

## 背景と課題

セキュリティ要件により、TD の調査作業は特定の AWS Region(site: us01 / ap01 など)に
束縛された `tdx claude` で行う。複数リージョンを同時に調査すると設定が競合する:

- `tdx claude` は**カレントディレクトリに `.claude/settings.local.json` を生成**し、
  終了時に元へ復元する。同じディレクトリから AP01 と US01 の2セッションを起動すると
  互いに上書き・巻き戻しが起き、**別リージョンのプロキシに繋がる**(情報の混戦)
- `~/.claude`(MCP サーバー・td-skills プラグイン)と `~/.config/tdx` も全セッション共有

これをコンテナ隔離で構造的に防ぐ CLI を作る。

## 調査済みの技術的事実(tdx 2026.3.1 のコードで確認)

- `tdx claude` の動作:
  1. ローカルに pass-through プロキシを起動(ポート 4000〜)。転送先は
     `getEndpoint(site, 'llm-proxy')` で site ごとに決まり、auth は OAuth トークン
     (`getOAuthTokens(profile)`)または API キー。Keychain から資格情報を読む
  2. カレントディレクトリに `.claude/settings.local.json` を生成(終了時に復元)
  3. `ANTHROPIC_BASE_URL=http://127.0.0.1:<port>`、`ANTHROPIC_AUTH_TOKEN=tdx-managed-proxy`、
     `CLAUDE_CODE_USE_BEDROCK=false`、`CLAUDE_CODE_USE_VERTEX=false`、`TDX_PROFILE=<profile>`
     を設定して `claude` を spawn
  4. `~/.claude` に Treasure AI Docs MCP(https://docs.treasure.ai/mcp)と
     td-skills マーケットプレイス/プラグインをインストール(グローバル変更)
  5. **`CLAUDE_CONFIG_DIR` を尊重する**(`CLAUDE_CONFIG_DIR || join(homedir(), '.claude')`)
- `tdx use site <x>` は**ターミナルセッション単位**(~/.config/tdx/sessions、セッションID キー)。
  `--default` を付けるとグローバル(~/.config/tdx/tdx.json)に書く
- tdx のインストール: npm パッケージ `@treasuredata/tdx`(mise の node 24 配下で確認)
- ホストの利用可能ランタイム: Docker 29.3.1(/usr/local/bin/docker)と
  Apple container 0.4.1(/usr/local/bin/container)。podman なし
- Apple container 0.4.1 の実機で確認した Docker との差分:
  - inspect / list は Go テンプレート非対応。`--format json` をパースする
  - `--hostname` なし(ホスト名は常にコンテナ名)、`--init` なし(VM 内の vminitd が init)
  - **名前付きボリュームにイメージ内容の copy-up がない**(root 所有の空 ext4)
    → メインプロセスを root の `tcb-boot` にして所有権と .bashrc を初期化する設計に変更
  - `container build` は `-f` 省略時 CWD 基準で Dockerfile を探す。さらに
    **ビルドコンテキストが macOS の TMPDIR(/var/folders)配下だと読めない**
    → ビルドコンテキストは `~/Library/Caches/tcb/build` に展開
  - `exec -u <user>` はユーザー名解決するが HOME を設定しない → exec 時に明示
  - `volume create` は名前をオプションより先に書く必要がある
  - コンテナ削除は `delete --force`、状態は "running"/"stopped"、
    存在しないコンテナの inspect は空配列(exit 0)
- Apple container **1.0 系は JSON スキーマが変わっている**(ソースで確認):
  - `status` が文字列 → `{"state":"running","networks":[...],"startedDate":...}` の
    オブジェクトに(`ContainerStatus` 構造体)
  - volume の `name`/`labels` がトップレベル → `configuration` 配下にネスト
    (`VolumeResource` は id + configuration のみエンコード)
  - エラー文言も揺れる("not found" / "notFound")
  → internal/engine/apple.go は両スキーマを吸収する(appleStatus / appleVolume)
- 認証はホストでは Keychain 保存。**Linux コンテナ内では Keychain が使えない**ため、
  コンテナ内で `tdx auth setup` を実行して HOME ボリュームに永続化するか、
  `~/.config/tdx/.env` 相当を注入する必要がある(要検証: コンテナ内での認証情報の保存先)

## ゴール / 非ゴール

**ゴール**
- `tcb run <site>` 一発で、site 専用に隔離された環境の `tdx claude` に入れる
- site ごとに HOME(→ `~/.claude`, `~/.config/tdx`, キャッシュ)と作業ディレクトリが分離され、
  同時に何セッション開いても混戦しない
- どの site の環境にいるか常に視覚的に分かる

**非ゴール(初期リリースでは)**
- claude-settings-switcher との統合(将来的に box 一覧表示くらいはあり得るが疎結合を保つ)
- 社内配布・公式化

## アーキテクチャ

- **バックエンド**: Docker と Apple container の2実装(CLI を子プロセスで叩く。SDK 依存なし)。
  `internal/engine` の Engine インターフェースに抽象化し、自動検出(docker 優先)または
  `--backend docker|apple` / `TCB_BACKEND` で選択
- **イメージ**: リポジトリ内 Dockerfile(バイナリに埋め込み)。`node:24-slim` ベースに
  `@anthropic-ai/claude-code` と `@treasuredata/tdx` を既定 `@latest` でインストール
  (+ git, ripgrep 等 Claude Code が使う最低限のツール)。
  `--rebuild` は --no-cache でビルドするため最新に追従できる。
  固定したい場合は `TCB_TDX_VERSION` / `TCB_CLAUDE_CODE_VERSION` で build-arg を上書き。
  ツールを追加したい場合は `~/.config/tcb/Dockerfile`(FROM tcb:base)で
  カスタム層を重ねられる(カスタム層は run のたびにキャッシュ付きビルド)
- **隔離の単位 = site**:
  - 名前付きボリューム `tcb-<site>-home` を `/home/tcb` にマウント
    → `~/.claude`・`~/.config/tdx`・認証情報・プラグインが site ごとに完全分離
  - 作業ディレクトリ: 既定 `~/tcb/<site>` をホスト側に作り `/work` にバインドマウント。
    `--dir <path>` で任意の調査ディレクトリを指定可能
  - `tcb run` の実行ディレクトリに `.claude/settings.json` がある場合は
    `/work/.claude/settings.json` へ同期する。site 固有に書き換わる
    `settings.local.json` は同期せず、従来通り workdir 分離に任せる
- **コンテナ命名**: `tcb-<site>`(1 site 1 コンテナ。同 site の2重起動は `docker exec` で同居)
- **エントリポイント**: 初回は `tdx auth setup` へ誘導 → `tdx use site <site> --default`
  (コンテナ内 HOME なので --default で安全)→ `tdx claude`
- **ガードレール**:
  - ボリューム初期化時に site 名をマーカーファイル(`/home/tcb/.tcb-site`)に記録し、
    異なる site での起動を拒否
  - コンテナ内プロンプト/ホスト名に site を表示(`hostname=ap01.tcb`、PS1 に site)
- **ネットワーク**: 初期はデフォルト。将来 egress 制限を検討(未決事項参照)

## CLI 仕様

```
tcb run <site> [--dir <path>] [--rebuild] [-- <tdx claude args...>]
    site 用コンテナを起動(なければ作成)して tdx claude に attach。
    2回目以降は既存コンテナに exec。--rebuild でイメージ再ビルド
tcb ls
    box 一覧(site / 状態 / 作業ディレクトリ / 起動時刻)
tcb shell <site>
    box に bash で入る(デバッグ・auth setup 用)
tcb stop <site>
tcb rm <box> [--keep-volume] [--force]
    box を削除。既定でコンテナと HOME ボリュームの両方を消す
    (認証情報も消える旨を警告して確認プロンプト)。--keep-volume でボリューム保持
tcb doctor
    docker の有無、イメージ、各 box の状態、site マーカーの整合性を診断
```

## 実装方針

配布のしやすさを優先して **Go**(単一バイナリ、`go install` / GitHub Releases 配布):

- 依存は標準ライブラリのみ(CLI パースは自前の軽量処理、テーブル出力は `text/tabwriter`)
- **Dockerfile と entrypoint.sh は `go:embed` でバイナリに埋め込む**
  → リポジトリを clone しなくても `tcb` 単体でイメージをビルドできる
- Lint/format: `gofmt` + `go vet`。テスト: `go test`(docker 呼び出しは `Runner`
  インターフェースに隔離してフェイクでテスト。実 docker を叩く統合テストは
  `TCB_E2E=1` でオプトイン)
- 配布: `go install github.com/mickeey2525/tdx-claude-box/cmd/tcb@latest`、
  将来的に goreleaser で各 OS バイナリを Releases に添付

## ディレクトリ構成(予定)

```
cmd/tcb/main.go       # エントリポイント
internal/
  cli/cli.go          # サブコマンドディスパッチ、--backend / TCB_BACKEND 解釈
  commands/           # run / ls / shell / stop / rm / doctor
  engine/             # Engine インターフェース + docker / apple 実装
  config/config.go    # 既定値(イメージタグ、workdir ルート、命名規則)
  site/site.go        # site 名バリデーション
image/
  embed.go            # go:embed でビルドコンテキストを埋め込み
  Dockerfile
  boot.sh             # メインプロセス(root)。HOME ボリューム初期化 → 常駐
  entrypoint.sh       # site マーカー検証 → 初回 auth 誘導 → tdx use site → tdx claude
PLAN.md               # 本ファイル
```

## マイルストーン

1. **M1: 最小動作** — Dockerfile + `tcb run <site>` + `tcb ls` + `tcb stop`。
   コンテナ内で `tdx auth setup` → `tdx claude` が site 固定で動くことを手動確認
2. **M2: ガードレール** — site マーカー検証、プロンプト/ホスト名表示、`tcb rm --volumes` の
   確認プロンプト、`tcb doctor`
3. **M3: 品質** — gofmt / go vet / go test / CI(GitHub Actions、対応済み)、README
4. **M4(任意)**: `--no-container` 軽量モード(`CLAUDE_CONFIG_DIR=~/.tcb/<site>/claude` +
   site 別 workdir 強制でホスト上分離)。Apple container バックエンドは対応済み

## 未決事項(実装時に判断)

1. **コンテナ内認証の方式**(解決済み・実機確認):
   - **`tdx auth setup` はコンテナ内では成立しない**:
     - ブラウザ SSO: OAuth コールバックがコンテナ内 localhost の一時ポートに返る
       ため届かない。さらに xdg-open がなく node がクラッシュ(→シム追加)
     - API キー方式: tdx は @napi-rs/keyring で OS の Secret Service に保存する
       が、コンテナに D-Bus/Secret Service がなく PermissionDenied で保存失敗
   - tdx 2026.6.5 の credential 解決順: TDX_API_KEY_<PROFILE> env →
     **TDX_API_KEY env** → keychain。`~/.config/tdx/.env` はコード内コメントに
     あるが現バージョンでは読まれない(実機確認)
   - → entrypoint が初回に API キーを聞いて検証(`tdx auth status`)し、
     `~/.config/tdx/.env`(0600、HOME ボリューム内)に保存。セッション開始時に
     source して `TDX_API_KEY` として渡す。`tcb shell` 用に .bashrc でも source
2. **`tdx claude` の Claude Code バージョンチェック**: 既定 @latest + `--rebuild`
   (--no-cache)で追従する方針にした。残る検証は MIN_CLAUDE_VERSION 警告が
   出たときに `tcb run <site> --rebuild` の案内で十分かの確認のみ
3. **プロキシのポート**: pass-through プロキシはコンテナ内 127.0.0.1 なので
   ホストと衝突しない(公開ポート不要)はず。attach 方式(tty)の確認のみ
4. **ネットワーク制限**: site ごとに egress を TD エンドポイントに絞るか
   (セキュリティ要件次第。iptables / docker network で可能だが複雑化する)
5. **git 認証**: 調査ディレクトリで git push 等が必要なら ssh-agent ソケットの
   マウントを `--ssh` オプションで提供するか

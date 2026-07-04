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
- Apple container バックエンド(M2 以降。まず Docker のみ)
- 社内配布・公式化

## アーキテクチャ

- **バックエンド**: Docker(`docker` CLI を子プロセスで叩く。Docker SDK 依存は持たない)
- **イメージ**: リポジトリ内 Dockerfile。`node:24-slim` ベースに
  `@anthropic-ai/claude-code` と `@treasuredata/tdx` を**バージョン固定**でインストール
  (+ git, ripgrep 等 Claude Code が使う最低限のツール)
- **隔離の単位 = site**:
  - 名前付きボリューム `tcb-<site>-home` を `/home/tcb` にマウント
    → `~/.claude`・`~/.config/tdx`・認証情報・プラグインが site ごとに完全分離
  - 作業ディレクトリ: 既定 `~/tcb/<site>` をホスト側に作り `/work` にバインドマウント。
    `--dir <path>` で任意の調査ディレクトリを指定可能
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
tcb rm <site> [--volumes]
    コンテナ削除。--volumes で HOME ボリュームごと削除(認証情報も消える旨を確認プロンプト)
tcb doctor
    docker の有無、イメージ、各 box の状態、site マーカーの整合性を診断
```

## 実装方針

claude-settings-switcher と同じツールチェーン(慣れているため):

- TypeScript、Node >= 24 ネイティブ実行(ビルドなし、相対 import は `.ts` 拡張子、
  erasableSyntaxOnly)。CLI パーサーは `node:util` の `parseArgs`(依存追加なし)
- Lint/format: Biome。テスト: `node --test`(docker 呼び出しは薄いラッパー関数に隔離して
  モック可能にする。実 docker を叩く統合テストは `TCB_E2E=1` でオプトイン)
- 配布: `npm link` / `npm install -g`(bin: `tcb`)。package.json に `"bin"` 設定

## ディレクトリ構成(予定)

```
src/
  cli.ts          # parseArgs とサブコマンドディスパッチのみ
  commands/       # run / ls / shell / stop / rm / doctor
  docker.ts       # docker CLI ラッパー(spawn、存在チェック、エラー整形)
  config.ts       # 既定値と ~/.config/tcb/config.json(イメージタグ、workdir ルート等)
  site.ts         # site 名バリデーション、マーカーファイル検証
image/
  Dockerfile
  entrypoint.sh   # site マーカー検証 → 初回 auth 誘導 → tdx use site → tdx claude
test/
PLAN.md           # 本ファイル
```

## マイルストーン

1. **M1: 最小動作** — Dockerfile + `tcb run <site>` + `tcb ls` + `tcb stop`。
   コンテナ内で `tdx auth setup` → `tdx claude` が site 固定で動くことを手動確認
2. **M2: ガードレール** — site マーカー検証、プロンプト/ホスト名表示、`tcb rm --volumes` の
   確認プロンプト、`tcb doctor`
3. **M3: 品質** — Biome / node:test / CI(GitHub Actions)、README
4. **M4(任意)**: `--no-container` 軽量モード(`CLAUDE_CONFIG_DIR=~/.tcb/<site>/claude` +
   site 別 workdir 強制でホスト上分離)、Apple container バックエンド

## 未決事項(実装時に判断)

1. **コンテナ内認証の方式**: `tdx auth setup` がコンテナ内(Keychain なし)でどこに
   資格情報を保存するか要検証。`~/.config/tdx/.env` にフォールバックするなら
   HOME ボリュームで完結。しない場合はホストから `.env` を生成して注入する仕組みが必要
2. **`tdx claude` の Claude Code バージョンチェック**: イメージ内の claude を
   どう更新するか(`--rebuild` 運用で足りるか、MIN_CLAUDE_VERSION との追従)
3. **プロキシのポート**: pass-through プロキシはコンテナ内 127.0.0.1 なので
   ホストと衝突しない(公開ポート不要)はず。attach 方式(tty)の確認のみ
4. **ネットワーク制限**: site ごとに egress を TD エンドポイントに絞るか
   (セキュリティ要件次第。iptables / docker network で可能だが複雑化する)
5. **git 認証**: 調査ディレクトリで git push 等が必要なら ssh-agent ソケットの
   マウントを `--ssh` オプションで提供するか

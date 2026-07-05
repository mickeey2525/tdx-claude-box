# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`tcb` は `tdx claude`(Treasure Data の Claude Code ラッパー)を **site ごとに隔離されたコンテナ**で動かす Go 製 CLI。複数リージョン(us01/ap01 など)を同時調査したときに `~/.claude`・`~/.config/tdx`・`.claude/settings.local.json` が競合して別リージョンのプロキシに繋がる事故を、コンテナ隔離で構造的に防ぐ。設計判断と実機調査の記録は [PLAN.md](PLAN.md) にある(特に「調査済みの技術的事実」と「未決事項」)。

## Commands

```sh
go test ./...                                 # 全テスト(実 docker 不要、フェイクで動く)
go test ./internal/commands -run TestRm       # 単一テスト
gofmt -l . && go vet ./...                    # lint 相当(CI はまだない)
go install ./cmd/tcb                          # ~/go/bin/tcb にインストール
bash -n image/entrypoint.sh image/boot.sh     # シェルスクリプトの構文チェック
```

イメージ関連:
- `image/` の Dockerfile・スクリプトは **go:embed でバイナリに埋め込まれる**。`image/*` を変更したら `go install` でバイナリも作り直すこと(バイナリ内の埋め込みが古いままになる)
- ユーザー向けの `tcb run <box> --rebuild` は `--no-cache` ビルド(npm の @latest を実際に更新するため。5分程度かかる)
- 開発中にスクリプト変更だけ試すなら `docker build -t tcb:latest image/` がキャッシュが効いて速い(埋め込みと同内容)
- **カスタム Dockerfile**(`~/.config/tcb/Dockerfile` / `TCB_DOCKERFILE`)がある場合、埋め込みイメージは `tcb:base` になり、その上のカスタム層が `tcb:latest` になる。カスタム層は `tcb run` のたびにキャッシュ付きでビルドされる(`internal/commands/run.go` の `ensureImage`)

## Architecture

依存ゼロ(標準ライブラリのみ)。レイヤは一方向:

```
cmd/tcb → internal/cli(ディスパッチ、--backend/TCB_BACKEND 解釈)
        → internal/commands(run/ls/shell/stop/rm/doctor)
        → internal/engine(Engine インターフェース)
            ├── docker.go  … Go テンプレート(--format)でパース
            └── apple.go   … Apple container CLI。--format json を JSON パース
```

- **internal/engine**: コンテナランタイム抽象。全ランタイム呼び出しは `Runner` インターフェース(Output/Interactive)経由で、テストは `fakeRunner` を注入する。新しい操作を足すときは Engine インターフェース+両実装+両テストをセットで
- **internal/config**: 命名規則とラベルの一元管理。コンテナ `tcb-<box>`、ボリューム `tcb-<box>-home`、ラベル `tcb.site`(box名)/ `tcb.workdir` / `tcb.tdsite`(実際のTD site)
- **internal/site**: box 名バリデーションと `DeriveTDSite`(box 名 `us01-7060` → TD site `us01` の自動導出。公式 site リストをハードコード)
- **box の概念**: 隔離単位は「box」で、box 名と TD site は別物(`--site` で明示、通常は自動導出)。1 box = 1 コンテナ + 1 HOME ボリューム。同 box への2セッション目は exec で同居

### コンテナ内のライフサイクル(image/)

- `boot.sh`(tcb-boot): コンテナのメインプロセス。**root で** HOME ボリュームを初期化(chown、skel、PS1、.env source を .bashrc へ)して常駐。Apple container のボリュームには Docker のような イメージ内容 copy-up がないため必須
- セッションは `exec --user tcb` + `HOME` 明示で入る(Apple の exec は user 指定でも HOME を設定しない)
- `entrypoint.sh`(tcb-entry): ガードレール(ボリューム内マーカー `.tcb-site` / `.tcb-td-site` の照合)→ `tdx use site` → 初回は API キーを聞いて検証・保存 → `exec tdx claude`

### 認証の制約(重要、変更時は要注意)

コンテナ内では `tdx auth setup` は使えない: ブラウザ SSO は OAuth コールバックが届かず、API キー保存も OS の Secret Service(D-Bus)がなく PermissionDenied になる。tdx が確実に読むのは **`TDX_API_KEY` 環境変数**のみ(`~/.config/tdx/.env` ファイルは tdx 自身には読まれない)。そのため entrypoint がキーを `~/.config/tdx/.env` に保存し、セッション開始時に source して env として渡している。検証は `tdx auth status` の **exit code** で行う(出力文言は setup と status で異なるためマッチしない)。

### プラットフォーム差分

Apple container 0.4.1 と Docker の差分(--hostname/--init なし、volume create の引数順、macOS TMPDIR がビルダーから読めない等)は PLAN.md の「調査済みの技術的事実」に列挙してある。ビルドコンテキストを `~/Library/Caches/tcb/build` に展開しているのはこのため。

## Conventions

- コード内コメント・コミュニケーションは日本語、CLI のユーザー向けメッセージは英語
- 破壊的操作(`tcb rm` のボリューム削除)は警告+確認プロンプトが既定。テストで担保している
- 実機で確認した挙動(tdx の内部仕様、Apple container の癖)を前提にした実装が多い。前提が変わったら PLAN.md も更新すること

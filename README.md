# pix-userland-mirakc-tuner

PIX-DT295 を mirakc のチューナーとして使うための Go 製コマンドと Docker イメージです。イメージには mirakc、`pix-mirakc-tuner`、現行の `pix-ts` クライアントを入れます。USB デバイスを所有する `pixd` は Linux ホスト上で動かし、コンテナとは `/run/pixd` の Unix socket だけを共有します。EPGStation、外部 B25 サーバー、PC/SC カードリーダーは含みません。

`pix-mirakc-tuner` は pix-userland 向けのチューナーコマンドを Go で実装したものです。mirakc が渡す種別・チャンネル・期間を検査して `pix-ts` に `exec` します。TS は `pix-ts` の標準出力から mirakc へ直接流れ、診断と終了状態もそのまま伝わります。

## 前提

- Linux x86_64 と Docker Compose。現在のイメージビルドは `linux/amd64` です。
- USB に接続した PIX-DT295、受信設備、適切な機器用 assets、およびホスト上の [pix-userland](https://github.com/Khronos31/pix-userland) の `pixd`。このリポジトリとイメージには assets を含めません。
- ホストの `pixd` が `--instance dt295 --runtime-dir /run/pixd --group` で起動していること。`pixd` とコンテナの `pix-ts` は同じ Core/IPC 版を使います。イメージ内の `pix-ts` は [pix-userland の `main`](https://github.com/Khronos31/pix-userland/tree/main) のコミットを Dockerfile の `PIX_USERLAND_REF` に固定してビルドします。ホスト側も同じコミットの `pixd` を使ってください。

機器の selector は `pixd --list-json` の実際の結果から選んでください。起動例は次の形です。`/run/pixd` の作成・権限設定は systemd の `RuntimeDirectory=pixd` などで行います。

```sh
pixd --device serial:YOUR_DEVICE_SERIAL \
  --assets /absolute/path/to/assets \
  --instance dt295 --runtime-dir /run/pixd --group
```

## Compose で起動

`config.yml` の `T27` を地域の物理チャンネルに変更します。ホストの `pixd` が起動し、`/run/pixd` が存在する状態で実行します。通常の rootful Docker なら Compose の既定ユーザー `0:0` でホスト socket に接続できます。コンテナの UID/GID を制限する場合は `.env.example` を `.env` にコピーし、ホストの `pixd` ユーザーに合わせて `PIX_UID` と `PIX_GID` を設定します。

```sh
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:40772/api/tuners
curl -fsS http://127.0.0.1:40772/api/services
```

公開イメージを使う場合は `docker compose pull` の後、`docker compose up -d --no-build` で起動します。Compose は HTTP API を既定で `40772` で公開し、EPG キャッシュを named volume に保存します。`.env` の `MIRAKC_PORT` と `PIX_RUNTIME_DIR` でホスト側のポートと socket ディレクトリを変更できます。`/api/tuners` の応答や healthcheck だけでは受信成功は分からないため、チャンネルストリームとサービスの認識も確認してください。

```sh
curl -fsS --max-time 5 \
  'http://127.0.0.1:40772/api/channels/GR/T27/stream?decode=0' \
  -o /tmp/pix-mirakc-check.ts
```

この `curl` は連続ストリームを5秒で切るので exit 28 はあり得ます。保存ファイルを検査し、mirakc のチューナーが free に戻って再取得できることを確認します。録画や raw TS をこのリポジトリへ追加しないでください。

## 接続の仕組み

`config.yml` の `tuners[].command` には mirakc の `{{{channel_type}}}`、`{{{channel}}}`、`{{{duration}}}` を渡します。例えば `GR` と `T27` は次に変換されます。

```sh
pix-ts --instance dt295 --system isdb-t --channel T27 \
  --time - --output - --runtime-dir /run/pixd --group
```

BS の `BS01_0` は `--channel BS01 --slot 0`、CS の `CS22@29024` は `--channel CS22 --tsid 29024` になります。衛星 slot は DT295 の 0〜7 の範囲のみ受け付けます。BS/CS は実機の確認が済むまで `config.yml` には登録していません。1台の物理チューナーを複数の `tuners` エントリーに分けて登録しないでください。

PIX-DT295 の本体内カード経路を使う前提で `decoded: true` とし、mirakc の外部 `decode-filter` は空にしています。`decoded: true` は設定上の出力契約であり、復号成功を保証するものではありません。実機ではサービス別の再生まで確認が必要です。

## ビルドと検査

```sh
go test ./...
go build -o pix-mirakc-tuner .
docker compose config --quiet
docker build -t pix-userland-mirakc-tuner:local .
python3 tests/docker_smoke.py
```

Go テストは合成 TS を出す mock `pix-ts` を使い、引数変換、TS バイト列、標準エラー、終了状態を確認します。Docker smoke test は同じく合成 TS を mirakc に流し、HTTP ストリーム、サービス認識、切断後の再利用を確認します。Docker ビルドは pix-userland の必要なソースだけを sparse checkout し、`assets/` を取得しません。GitHub Actions は Go テストと Docker smoke test が通った後、`main` への push で `ghcr.io/yude/pix-userland-mirakc-tuner:main` とコミット SHA タグを公開します。イメージには mirakc・アダプター・`pix-ts` のみを含め、`pixd` と機器用 assets は含めません。

現時点の自動検査は合成 TS とビルドまでです。DT295 実機による Linux 上の受信、mirakc のサービス・EPG認識、長時間稼働は別途確認が必要です。

## ライセンス

アダプターのコードは MIT です。イメージ内の mirakc、pix-userland、Core、libusb には各プロジェクトのライセンスが適用されます。`/usr/share/doc/pix-userland-mirakc-tuner/licenses/` にアダプター、pix-userland、Core adapter、libusb のライセンス・来歴を配置します。Core の一部 adapter は GPL-2.0-only です。

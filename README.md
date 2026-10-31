# pix-userland-mirakc-tuner

pix-userland が対応しているチューナーを mirakc から使うための Go 製コマンドと Docker イメージです。イメージには mirakc、`pix-mirakc-tuner`、未改造の `pixd`・`pixctl`・`pix-ts`、起動用の `pixd-launch` を入れます。USBを所有するpixdはホスト常駐と別コンテナの両方を選べます。pix-userland本体へのパッチは不要です。

`pix-mirakc-tuner` は pix-userland 向けのチューナーコマンドを Go で実装したものです。mirakc が渡す種別・チャンネル・期間を検査して `pix-ts` に `exec` します。TS は `pix-ts` の標準出力から mirakc へ直接流れ、診断と終了状態もそのまま伝わります。

## 前提

- Docker Compose。イメージは `linux/amd64`、`linux/arm64`、`linux/arm/v7` 向けにビルドします。armv6 (`linux/arm/v6`) は mirakc の公式ベースイメージが対応していないため含めません。
- USB に接続した PIX-DT295、受信設備、および [pix-userland](https://github.com/Khronos31/pix-userland) に含まれる正規の `firmware/dt295.bin`。このリポジトリとイメージにはファームウェアを含めません。
- `pixd` とコンテナの `pix-ts` は同じ Core/IPC 版を使います。pix-userlandのソースはDockerfileの `PIX_USERLAND_REF` に固定して無変更でビルドします。ホスト常駐を選ぶ場合はホスト側も同じコミットを使い、`--instance dt295 --runtime-dir /run/pixd --group` で起動してください。pixdもコンテナ化する場合は同一イメージなので版がそろいます。

機器の selector は `pixd --list-json` の実際の結果から選んでください。起動例は次の形です。`/run/pixd` の作成・権限設定は systemd の `RuntimeDirectory=pixd` などで行います。

```sh
pixd --device serial:YOUR_DEVICE_SERIAL \
  --firmware /absolute/path/to/pix-userland/firmware/dt295.bin \
  --instance dt295 --runtime-dir /run/pixd --group
```

## Docker Compose で起動

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

この `curl` は連続ストリームを5秒で切るので exit code が 28 になり得ます。保存ファイルを検査し、mirakc のチューナーが free に戻って再取得できることを確認します。録画や raw TS をこのリポジトリへ追加しないでください。

## pixd もコンテナで起動する

[`compose.usb.yaml`](compose.usb.yaml) はDebianなどのLinuxホスト上のrootful Docker向けです。既存の `compose.yaml` と併用せず、どちらか一方を使います。ホストで同じ機器を保持しているpixdサービスがある場合は先に停止してください。

```sh
cp -n .env.usb.example .env
# .envのPIX_DEVICEとPIX_FIRMWARE_FILEを実際の値に編集する
docker compose -f compose.usb.yaml config --quiet
docker compose -f compose.usb.yaml up -d --build
docker compose -f compose.usb.yaml ps
docker compose -f compose.usb.yaml logs --tail 50 pixd
docker compose -f compose.usb.yaml exec pixd pixctl --instance dt295 --runtime-dir /run/pixd --group status
```

`.env` がすでにある場合は [`.env.usb.example`](.env.usb.example) の項目を追記してください。`PIX_FIRMWARE_FILE` はホストに存在する `firmware/dt295.bin` の絶対パスです。ファームウェアは読み取り専用で渡し、イメージやGitへ追加しません。ホスト用のsystemd unitや専用ユーザーは不要です。

pixdコンテナはrootで動作し、USBのbind mountと `device_cgroup_rules` を使います。`privileged`、追加capability、ホストネットワークは設定しません。`/dev/bus/usb` 全体とmajor 189のアクセスを許可するため、コンテナからは他のUSBデバイスにもアクセス可能です。cgroupでVID/PID単位に制限する機能はありません。mirakcコンテナにはUSBを渡さず、named volume `pixd-ipc` のUnix socketだけを共有します。pixd再起動でもvolume自体は保持されます。

### シリアル指定

`PIX_DEVICE=serial:YOUR_SERIAL` を指定すると、**このリポジトリの** `pixd-launch` が `/sys/bus/usb/devices` の保持情報を読み、対応するPIX機器の `usb:BUS-PORT` に変換してから既存pixdを `exec` します。これによりカーネルはシリアルを認識しているのにUSBから再読取りできない場合も、pix-userland本体を変更せずに選択できます。

- sysfsに同じシリアルのPIX機器が複数あればエラーにします。
- 一致する保持情報がない場合は元の `serial:` をpixdへ渡し、既存の読取りに任せます。
- `PIX_DEVICE=usb:1-8.2` のようなポート指定もそのまま使えます。
- シリアルの解決は起動時だけです。抜き差しやポート変更後はpixdコンテナを再起動すると再解決します。
- `pixd --list-json` の出力は変更しないため、そこには `serial: null` が残る場合があります。Linux側の値はホストの `cat /sys/bus/usb/devices/1-8.2/serial` などで確認できます。

1台につき1つのpixdサービスを使います。複数台ではサービスごとに異なるselector、instance、IPC volumeを設定し、mirakcにも対応するチューナーコマンドを追加してください。同じselectorを指定した `--scale pixd=2` は使えません。

### 停止・復旧

`pixd-launch` はプロセスを置き換えるので、Composeの停止シグナルはpixdへ届きます。30秒以内に終了しなければDockerが強制終了します。pixdの異常終了は `on-failure:3` で再試行します。healthcheckはIPCに応答することを確認するもので、実際の受信・復号成功を保証せず、unhealthyになっただけでDockerが自動再起動するものでもありません。

```sh
docker compose -f compose.usb.yaml restart pixd
docker compose -f compose.usb.yaml exec pixd pixctl --instance dt295 --runtime-dir /run/pixd --group status
```

`depends_on` は初回起動時の順序を制御します。稼働中のpixd再起動では進行中のTSは切れます。次のストリーム要求で再取得できるか確認してください。USB機器やカーネル側に異常が残る場合はコンテナの再起動だけでは復旧しないことがあり、実機の再接続が必要です。DT295実機でのコンテナ受信・抜き差し後の復旧は未検証です。

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
docker build --pull -t pix-userland-mirakc-tuner:local .
python3 tests/docker_smoke.py
python3 tests/pixd_smoke.py
```

Go はビルド時点の最新安定版を使います。Docker は `golang:alpine` を使用し、Actions は `--pull` でベースイメージを更新します。Go テストも `setup-go` の `stable` を使います。arm64・armv7 のイメージは `ubuntu-26.04-arm` ランナーでビルドし、armv7 の実行には QEMU を使います。

Go テストは合成 TS を出すmock pix-tsの引数・出力と、pixd-launchの合成sysfsによるシリアル解決、重複拒否、引数維持、終了コード、SIGTERM/SIGINTを検査します。Dockerでは合成TSを使ったmirakcのHTTPストリーム・サービス認識・再利用に加え、pixdのCLI起動、ラッパーからのexec、重複拒否、コンテナ停止を検査します。Dockerビルドは必要なソースとファームウェアのmanifestだけをsparse checkoutし、`assets/` やファームウェア本体を取得しません。GitHub Actionsは各プラットフォームの検査後、mainへのpushでGHCRのmainタグとコミットSHAタグを公開します。

現時点の自動検査は合成 TS とビルドまでです。DT295 実機による Linux 上の受信、mirakc のサービス・EPG認識、長時間稼働は別途確認が必要です。

## ライセンス

アダプターのコードは MIT です。イメージ内の mirakc、pix-userland、Core、libusb には各プロジェクトのライセンスが適用されます。`/usr/share/doc/pix-userland-mirakc-tuner/licenses/` にアダプター、pix-userland、Core adapter、libusb のライセンス・来歴を配置します。Core の一部 adapter は GPL-2.0-only です。

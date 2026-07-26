# quakeguard

緊急地震速報(EEW)を受信して、揺れが到達する前にWindowsマシンのHDDのヘッドを退避させる常駐ツールです。

P2P地震情報のWebSocket APIから**緊急地震速報(警報)**(予想震度5弱以上)を受信し、対象のHDDに ATA `STANDBY IMMEDIATE` を発行してヘッドをランプに退避させます。オプションで、保護中にOSのI/Oがディスクを起こしてしまわないよう、ボリュームのロック+ディスマウントまで行えます。

## 仕組み

```
P2P地震情報 (wss://api.p2pquake.net/v2/ws)
        │  緊急地震速報(警報) code:556
        ▼
  地域フィルタ (watchPrefs)
        ▼
  保護モード発動 ──── standby  : ボリュームflush → ATA STANDBY IMMEDIATE
        │            dismount : flush → ロック → ディスマウント → STANDBY
        ▼                       (保護中はI/Oがディスクに届かない)
  holdSeconds 経過後に自動解除(次のアクセスで自動再マウント・スピンアップ)
```

- 保護中に続報を受信した場合は保持時間が延長されます。
- HDDの耐衝撃性は動作時 約60〜70G に対しヘッド退避時 約250〜350G で、退避には実効性があります。
- SSD/NVMeは自動選択の対象外です(退避の意味がないため)。

## モードの選択

| mode | 動作 | 向いている場合 |
|---|---|---|
| `standby` | flush+ヘッド退避のみ。以後のI/Oでディスクは起きる | 影響を最小にしたい場合 |
| `dismount`(既定) | ボリュームをロック・ディスマウントして保持。保護中はI/O不可 | 揺れの間ヘッド退避を確実に維持したい場合 |

`dismount` は保護中、対象ドライブ上のファイルを開いているアプリのハンドルを無効化します(録画・ダウンロード等はエラーになります)。震度5弱以上が予想される状況ではファイルエラーの方がましである、という割り切りに基づく設計です。

## ビルド

WSL/Linuxからのクロスコンパイル:

```bash
GOOS=windows GOARCH=amd64 go build -o quakeguard.exe ./cmd/quakeguard
```

## 使い方

**管理者権限のコマンドプロンプト/PowerShellで実行してください**(物理ディスクへのIOCTL発行に必要です)。

```bat
: ディスク一覧の確認
quakeguard.exe -list-disks

: 動作確認(実際にはディスクを触らない)
quakeguard.exe -dry-run -simulate

: 保護の実地テスト(1回だけ保護→数秒後に解除)
quakeguard.exe -simulate

: 常駐開始
quakeguard.exe -config config.json
```

自動起動はタスクスケジューラに「最上位の特権で実行」で登録するのが手軽です:

```bat
schtasks /create /tn quakeguard /tr "C:\path\to\quakeguard.exe -config C:\path\to\config.json" /sc onstart /ru SYSTEM /rl highest
```

## 設定 (config.json)

[config.example.json](config.example.json) をコピーして編集してください。

| キー | 既定値 | 説明 |
|---|---|---|
| `sourceUrl` | P2P地震情報 v2 | EEW受信WebSocketエンドポイント |
| `watchPrefs` | `[]`(全国) | この都道府県が警報対象地域に含まれるときだけ発動(例 `["東京都"]`) |
| `mode` | `dismount` | `standby` / `dismount` |
| `holdSeconds` | `180` | 最後のトリガーから保護を維持する秒数 |
| `disks` | `[]`(自動) | 対象の物理ディスク番号を明示指定。自動時は非システムHDD全部 |
| `excludeSystemDisk` | `true` | システムディスクを対象外にする |
| `allowTest` | `false` | 訓練・テスト報でも発動させる |
| `dryRun` | `false` | ディスク操作をログ出力のみにする |

## 制約・注意

- P2P地震情報のEEWは**警報(予想震度5弱以上)のみ**です。より小さい地震や予報クラスから発動したい場合は、受信部(`internal/eew.Source`)を差し替えて [dmdata.jp](https://dmdata.jp/)(有料、気象庁公式配信)に対応させてください。受信部はインターフェース分離済みです。
- システムドライブ(C:)はOSが常時I/Oを行うため保護できません(既定で対象外)。
- 直下型地震では猶予が数秒以下のことがあり、間に合わない場合があります。
- ATAパススルーはUSB接続の一部ブリッジチップでは通らないことがあります。`-simulate` で事前確認してください。

## 開発

```bash
go test -coverpkg=./... ./...   # カバレッジ98%以上
go vet ./...
GOOS=windows GOARCH=amd64 go build ./...
```

Windows専用のsyscall層(`internal/windisk`)は薄く保ち、IOCTLバッファの組み立て・解析(`internal/diskioctl`)や発動判定・保持ロジック(`internal/protect`, `internal/eew`)はOS非依存の純粋関数としてテストしています。非Windows環境では `-dry-run` でシミュレーションコントローラが動きます。

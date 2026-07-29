# eew-hddguard

緊急地震速報(EEW)を受信して、揺れが到達する前にWindowsマシンのHDDのヘッドを退避させる常駐ツールです。

P2P地震情報のWebSocket APIから**緊急地震速報(警報)**(予想震度5弱以上)を受信し、対象のHDDに ATA `STANDBY IMMEDIATE` を発行してヘッドをランプに退避させます。オプションで、保護中にOSのI/Oがディスクを起こしてしまわないよう、ボリュームのロック+ディスマウントまで行えます。

Windowsではタスクトレイに常駐し、以下を提供します:

- **トースト通知** — 保護の発動・解除時に通知(発動時はディスマウント中である旨と復旧方法を案内)
- **テスト実行** — トレイメニューから実際に保護を10秒間発動して、ヘッド退避が動くか確認
- **今すぐ解除** — 揺れが収まったらユーザ操作で保持時間を待たずに早期復旧(アイコンも待機/保護中で色が変わります)

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

[Go](https://go.dev/dl/)(1.22以降)が必要です。cgo不要の純Goなので、WindowsネイティブでもWSLからのクロスコンパイルでも同じバイナリができます。

Windows上で(PowerShell / コマンドプロンプト):

```powershell
go build -ldflags "-H=windowsgui" -o eew-hddguard.exe .\cmd\eew-hddguard
```

WSL/Linuxからのクロスコンパイル:

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o eew-hddguard.exe ./cmd/eew-hddguard
```

`-H=windowsgui` はトレイ常駐時にコンソール窓を出さないためのフラグです。コンソール起動時は親コンソールに接続して出力するので、付けたままでもCLIの結果は見えます。コンソール専用で使う場合は外しても構いません(`logFile` 設定でファイルにもログを残せます)。

## 使い方

**管理者権限のコマンドプロンプト/PowerShellで実行してください**(物理ディスクへのIOCTL発行に必要です)。

```bat
: ディスク一覧の確認
eew-hddguard.exe -list-disks

: 動作確認(実際にはディスクを触らない)
eew-hddguard.exe -dry-run -simulate

: 保護の実地テスト(1回だけ保護→数秒後に解除)
eew-hddguard.exe -simulate

: 常駐開始(タスクトレイにアイコンが出ます)
eew-hddguard.exe -config config.json

: トレイなしのコンソールモード
eew-hddguard.exe -config config.json -no-tray
```

常駐中はトレイアイコンの右クリックメニューから操作します:

| メニュー | 動作 |
|---|---|
| テスト実行 | 実際に保護を10秒間発動(flush→dismount→ヘッド退避→自動復旧)。導入時の動作確認用 |
| 今すぐ解除 | 保護中のみ有効。揺れが収まったら保持時間を待たずに復旧 |
| 終了 | 保護を解除してから終了 |

### 自動起動(タスクスケジューラ)

登録はPowerShell(管理者)で行います。`schtasks` ではなくPowerShellを使うのは、タスクスケジューラの既定にある**「タスクを停止するまでの時間: 3日」を無効化**しないと常駐タスクが3日で強制終了されるためです(`schtasks` にはこの設定を変えるフラグがありません)。

トレイアイコンとトースト通知は**ユーザーセッション**でしか表示できないため、トレイ運用では「ログオン時・自分のアカウント・最上位の特権」で登録します:

```powershell
$dir = "C:\path\to\eew-hddguard"
$action    = New-ScheduledTaskAction -Execute "$dir\eew-hddguard.exe" -Argument "-config config.json" -WorkingDirectory $dir
$trigger   = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$settings  = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName "eew-hddguard" -Action $action -Trigger $trigger -Settings $settings -Principal $principal
```

- `-LogonType Interactive`(GUIでは「ユーザーがログオンしているときのみ実行する」)が重要です。これ以外だとタスクは通知領域のない非対話セッションで実行され、トレイアイコンもトースト通知も表示できません(その場合アプリはトレイなしで保護動作を継続します)。
- `-ExecutionTimeLimit ([TimeSpan]::Zero)` が「停止するまでの時間」の無効化です。GUIで登録した場合は「設定」タブの「タスクを停止するまでの時間」のチェックを外してください。
- `-WorkingDirectory` を指定しておくと、`logFile` の相対パス(`logging.log`)が実行フォルダに作られます。
- バッテリー駆動でも保護が動くよう、電源条件も外しています。

ログオンせず稼働させるサーバ用途では、`-no-tray` を付けてSYSTEMでの起動時実行にします(トレイ・通知なし。ログは `logFile` で確認):

```powershell
$dir = "C:\path\to\eew-hddguard"
$action   = New-ScheduledTaskAction -Execute "$dir\eew-hddguard.exe" -Argument "-config config.json -no-tray" -WorkingDirectory $dir
$trigger  = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "eew-hddguard" -Action $action -Trigger $trigger -Settings $settings -User "SYSTEM" -RunLevel Highest
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
| `logFile` | (なし) | 指定するとログをファイルにも追記(トレイ常駐でコンソールがないとき用) |

運用のヒント:

- **`watchPrefs` は絞るのを推奨。** 空のままだと全国どこかで震度5弱以上の警報が出るたびに発動し、そのたびに数分間ボリュームが外れます。ヘッド退避自体は無害ですが、遠方の地震で毎回外れるのは煩わしいはずです。
- **受信経路まで含めた通し確認**をしたい場合は一時的に `allowTest: true` にすると、P2P地震情報の訓練・テスト報でも発動します。確認が済んだら `false` に戻してください。
- `-simulate` やトレイの「テスト実行」は実際にボリュームをdismountします。対象ドライブのファイルを開いているアプリ(録画・ダウンロード等)は先に閉じてください。
- `logFile` の相対パス(example の `logging.log` など)は**カレントディレクトリ基準**です。タスクスケジューラ起動はカレントが `C:\Windows\System32` になりがちなので、タスクの「開始(オプション)」に実行ファイルのフォルダを指定するか、絶対パス(例 `C:\\path\\to\\eew-hddguard\\logging.log`)を書いてください。

## 記憶域スペース(Storage Spaces)での動作

そのまま使えます。プールのメンバーHDDは `PhysicalDriveN` として列挙され(ボリューム0台のHDDとして自動選択に入る)、HDDで構成された仮想ディスクは `SPACE` として列挙されてその上のボリュームがdismount対象になります。保護は「全ボリュームの静止 → 全ディスクのヘッド退避」の2段階で行うため、仮想ディスク側のI/Oを塞いだ後にメンバーHDDが退避され、揺れの間I/Oで起こされることもありません。仮想ディスク自体はATAコマンドを受けないためSTANDBYの対象外です。

1点だけ注意: SSDで構成された `SPACE`(非回転扱い)は自動選択に入りません。**SSDとHDDが同一プールを共有している(階層化している)場合**は、SSD側のSpace経由のI/Oがプールのメタデータ更新でHDDに届き得るため、`disks` にそのSPACEのディスク番号も明示追加してボリュームごと静止させてください。プールが別々なら不要です。

## トラブルシューティング

**ディスク一覧が空/「access denied」になる**
物理ディスクを開くには管理者権限が必要です。管理者として開いたPowerShellから実行してください。権限不足で1台も開けなかった場合はその旨のエラーを表示します。

**CLIの出力がプロンプトの後に表示される**
PowerShellはGUIサブシステムのアプリ(`-H=windowsgui` でビルドしたexe)の終了を待たずにプロンプトを返すため、`-list-disks` などの出力が次のプロンプトの後に表示されることがあります。表示自体はされているので、そのままEnterで戻れます。

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

### CI / リリース

GitHub Actionsで、push/PRごとにvet・テスト(カバレッジ90%未満は失敗)・Windowsビルドを実行します([ci.yml](.github/workflows/ci.yml))。

リリースは `v` で始まるタグをpushすると自動で行われ、テスト後にexeをビルドしてzip(exe + config.example.json + README + LICENSE)をGitHub Releasesに添付します([release.yml](.github/workflows/release.yml)):

```bash
git tag v1.0.0
git push origin v1.0.0
```

リリースビルドにはタグ名がバージョンとして埋め込まれ、`eew-hddguard.exe -version` と起動時のログで確認できます。

## ライセンス

[Apache License 2.0](LICENSE)

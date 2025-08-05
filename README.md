# UNI'S ON AIR Speed Tracker

UNI'S ON AIRのランキング画面を自動で監視し、時速（ポイント変化率）を計測・追跡するツールです。Gemini AIでデータを抽出し、1h、6h、12h、24hの時間別ポイント変化を分析してDiscordに投稿します。

## ✨ 新機能

- **🖱️ GUI対応**: 直感的なグラフィカルユーザーインターフェース
- **📊 時速表示**: 1h、6h、12h、24h の時間別ポイント変化を表示
- **🎯 ドラッグ選択**: マウスでスクリーン領域を簡単選択
- **🔄 5つの領域対応**: Region 0-4まで対応（Region 0は自動フルスクリーン）
- **💾 データ出力**: JSON/CSV形式でデータを保存

## 🚀 セットアップ

### 1. 前提条件

- Go 1.21以上
- Windows環境（スクリーンショット機能のため）

### 2. 依存関係のインストール

```bash
go mod tidy
```

### 3. 環境変数の設定

`.env.example`をコピーして`.env`ファイルを作成し、必要なAPIキーを設定してください：

```bash
copy .env.example .env
```

`.env`ファイルを編集して以下の値を設定：
- `GEMINI_API_KEY`: Google Gemini APIキー（**必須**）
- `DISCORD_WEBHOOK_0~4`: Discord WebhookのURL（オプション）
- `DESIRED_MINUTES`: 実行タイミング（分）をカンマ区切りで指定（例: 1,15,30,45）

### 4. 設定ファイル

`config.json`ファイルで名前の置換設定を行います：

```json
{
  "name_replaces": {
    "誤認識される名前": "正しい名前"
  }
}
```

## 📁 ファイル構成

- `main.go`: メインプログラム
- `go.mod`: Go モジュール定義
- `config.json.example`: 設定ファイルのテンプレート（名前置換設定）
- `.env.example`: 環境変数のテンプレート（APIキーなど）
- `README.md`: このファイル
- `res/`: 出力ディレクトリ（スクリーンショット、JSON、CSV）

## 🎯 使用方法

### GUIモード（推奨）

```bash
go run main.go
```

またはビルドして実行：

```bash
go build -o unisonair-speed-tracker.exe
./unisonair-speed-tracker.exe
```

### GUI操作手順

1. **設定入力**
   - Gemini API Keyを入力（必須）
   - Discord Webhook URLを入力（オプション）
   - 実行タイミングを設定（例: 1,15,30,45）

2. **領域設定**
   - Region 0: 自動でフルスクリーン検出（Refreshボタンで更新）
   - Region 1-4: "Select"ボタンでドラッグ選択、または手動入力

3. **実行**
   - "Save Settings"で設定保存
   - "Start"でスケジュール実行開始
   - ログでリアルタイム状況確認

### CLIモード

```bash
go run main.go --cli
```

### 出力ファイル

実行後、以下にファイルが生成されます：
- `res/{region}/screenshot/`: スクリーンショット画像
- `res/{region}/json/datas.json`: 抽出データ（JSON形式）
- `res/{region}/csv/datas.csv`: 分析データ（CSV形式）

## 🔧 主要機能

### 📸 スクリーンショット機能
- **5つの領域対応**: Region 0（全画面自動）+ Region 1-4（カスタマイズ可能）
- **ドラッグ選択**: マウスで領域を簡単指定
- **自動スケジュール実行**: 指定した分（例: 1,15,30,45分）に自動実行

### 🤖 AI解析機能  
- **Gemini AI OCR**: Google Gemini 1.5 Flash でランキング情報を抽出
- **名前置換**: OCR誤認識を設定ファイルで自動修正
- **時速計算**: 1h、6h、12h、24h の時間別ポイント変化を表示

### 💾 データ管理
- **JSON保存**: 取得データを構造化して保存
- **CSV出力**: スプレッドシート用にCSV形式で出力
- **Discord連携**: 結果をDiscord Webhookに自動投稿

### 🖥️ ユーザーインターフェース
- **GUI操作**: 直感的なグラフィカルインターフェース
- **設定保存**: .envファイルで設定を永続化
- **リアルタイムログ**: 実行状況をリアルタイム表示

## 🛠️ トラブルシューティング

### よくあるエラー
- `GEMINI_API_KEY environment variable is not set`: `.env`ファイルにAPIキーが設定されていません
- `failed to capture screenshot`: スクリーンショット権限がない、または画面が見つかりません
- `Discord webhook failed`: Webhook URLが無効またはDiscordサーバーに接続できません

### パフォーマンス改善
- スクリーンショット時に画面を高解像度に設定してください
- Gemini API呼び出し頻度に注意してください（レート制限があります）

## 🔒 セキュリティ

以下のファイルには機密情報が含まれているため、Gitリポジトリには追加しないでください：
- `.env` - APIキーが含まれる
- `config.json` - 個人設定が含まれる
- `res/` ディレクトリ内の生成ファイル

## 📚 GitHub設定推奨

**リポジトリ名**: `unisonair-speed-tracker`  
**説明**: "UNI'S ON AIR ranking speed tracker with Gemini AI OCR and Discord integration"  
**トピック**: `unisonair`, `ranking`, `speed-tracker`, `gemini-ai`, `discord`, `golang`, `gui`
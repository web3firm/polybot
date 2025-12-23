# 🤖 Polybot

**Polymarket Arbitrage Alert Bot** - A professional-grade Go application that monitors Polymarket for arbitrage opportunities and sends real-time Telegram alerts.

![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)
![License](https://img.shields.io/badge/License-MIT-green)

## 🚀 Features

- **Real-time Monitoring** - Scans Polymarket every 30 seconds (configurable)
- **Instant Alerts** - Telegram notifications with actionable insights
- **Smart Detection** - Identifies underpriced and overpriced markets
- **Modern Bot** - Interactive Telegram bot with inline keyboards
- **Professional Architecture** - Clean, modular codebase ready for expansion
- **Future-Ready** - Trading engine stub for auto-trading capabilities

## 📊 How Arbitrage Works

On Polymarket, each market has YES and NO tokens. In a perfectly efficient market:
```
YES price + NO price = $1.00
```

When this doesn't hold true, there's an arbitrage opportunity:

| Situation | Total | Opportunity |
|-----------|-------|-------------|
| YES: $0.48 + NO: $0.49 | $0.97 | Buy both for guaranteed 3% profit |
| YES: $0.52 + NO: $0.51 | $1.03 | Overpriced - wait or short |

## 🏗️ Architecture

```
polybot/
├── cmd/
│   └── polybot/
│       └── main.go          # Application entrypoint
├── internal/
│   ├── bot/
│   │   └── telegram.go      # Telegram bot & commands
│   ├── config/
│   │   └── config.go        # Configuration management
│   ├── database/
│   │   └── database.go      # SQLite persistence
│   ├── polymarket/
│   │   └── client.go        # Polymarket API client
│   ├── scanner/
│   │   └── scanner.go       # Opportunity scanner
│   └── trading/
│       └── engine.go        # Trading engine (future)
├── .env.example             # Configuration template
├── go.mod                   # Go modules
└── README.md
```

## 🛠️ Setup

### Prerequisites

- Go 1.21+
- Telegram Bot Token (from [@BotFather](https://t.me/BotFather))
- Your Telegram Chat ID (from [@userinfobot](https://t.me/userinfobot))

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/yourusername/polybot.git
   cd polybot
   ```

2. **Copy environment file**
   ```bash
   cp .env.example .env
   ```

3. **Configure your settings**
   ```bash
   # Edit .env with your values
   TELEGRAM_BOT_TOKEN=your_token_here
   TELEGRAM_CHAT_ID=your_chat_id
   ```

4. **Install dependencies**
   ```bash
   go mod tidy
   ```

5. **Build**
   ```bash
   go build -o polybot ./cmd/polybot
   ```

6. **Run**
   ```bash
   ./polybot
   ```

## 📱 Telegram Commands

| Command | Description |
|---------|-------------|
| `/start` | Initialize bot & subscribe to alerts |
| `/help` | Show all commands |
| `/status` | Bot & market status |
| `/opportunities` | Current arbitrage opportunities |
| `/stats` | Statistics & P/L tracking |
| `/settings` | Customize alert preferences |
| `/subscribe` | Enable alerts |
| `/unsubscribe` | Disable alerts |

## ⚙️ Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `TELEGRAM_BOT_TOKEN` | - | Your Telegram bot token |
| `TELEGRAM_CHAT_ID` | - | Your Telegram chat ID |
| `SCAN_INTERVAL` | `30s` | How often to scan markets |
| `MIN_SPREAD_PCT` | `2` | Minimum spread % for alerts |
| `ALERT_COOLDOWN` | `5m` | Cooldown between repeat alerts |
| `MAX_MARKETS` | `0` | Max number of markets to scan (0 = unlimited) |
| `POLYMARKET_BATCH_SIZE` | `1000` | Polymarket API batch size per request |
| `POLYMARKET_MAX_RPS` | `5` | Max Polymarket API requests per second |
| `TRADING_ENABLED` | `false` | Enable auto-trading |
| `DRY_RUN` | `true` | Simulate trades without executing |

## 🔮 Roadmap

- [x] Polymarket API integration
- [x] Telegram bot with commands
- [x] Opportunity detection
- [x] SQLite persistence
- [ ] Auto-trading integration
- [ ] Multi-platform support (Kalshi, etc.)
- [ ] Web dashboard
- [ ] Position management
- [ ] Risk management

## ⚠️ Disclaimer

This software is for educational purposes only. Cryptocurrency and prediction market trading involves substantial risk. Use at your own risk. The authors are not responsible for any financial losses.

## 📄 License

MIT License - feel free to use and modify.

---

Built with 💜 for the degen community

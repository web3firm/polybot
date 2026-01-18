package bot

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

// ═══════════════════════════════════════════════════════════════════════════════
// TELEGRAM BOT - Modern trading notifications & control
// ═══════════════════════════════════════════════════════════════════════════════
//
// Features:
//   📊 Real-time signal alerts
//   💰 Trade notifications (open/close/TP/SL)
//   📈 Daily P&L summaries
//   🎛️ Bot control commands (/status, /pause, /resume, /stats)
//   🔔 Configurable alert levels
//
// ═══════════════════════════════════════════════════════════════════════════════

// TelegramBot manages the Telegram interface
type TelegramBot struct {
	mu      sync.RWMutex
	api     *tgbotapi.BotAPI
	chatID  int64
	running bool
	stopCh  chan struct{}

	// Stats for reporting
	statsProvider StatsProvider

	// Control callbacks
	onPause  func()
	onResume func()
}

// StatsProvider provides trading statistics
type StatsProvider interface {
	GetStats() (trades, wins, losses int, pnl, equity decimal.Decimal)
}

// PositionInfo represents a position for display
type PositionInfo struct {
	Asset      string
	Side       string
	Entry      decimal.Decimal
	Current    decimal.Decimal
	PnL        decimal.Decimal
	PnLPercent decimal.Decimal
	Duration   time.Duration
}

// NewTelegramBot creates a new Telegram bot
func NewTelegramBot(statsProvider StatsProvider) (*TelegramBot, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
	}

	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if chatIDStr == "" {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID not set")
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID: %w", err)
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	bot := &TelegramBot{
		api:           api,
		chatID:        chatID,
		stopCh:        make(chan struct{}),
		statsProvider: statsProvider,
	}

	log.Info().Str("username", api.Self.UserName).Msg("🤖 Telegram bot initialized")

	return bot, nil
}

// SetControlCallbacks sets pause/resume handlers
func (b *TelegramBot) SetControlCallbacks(onPause, onResume func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onPause = onPause
	b.onResume = onResume
}

// Start begins listening for commands
func (b *TelegramBot) Start() {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.mu.Unlock()

	go b.commandLoop()
	log.Info().Msg("📱 Telegram bot started")
}

// Stop stops the bot
func (b *TelegramBot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return
	}

	b.running = false
	close(b.stopCh)
	log.Info().Msg("Telegram bot stopped")
}

// ═══════════════════════════════════════════════════════════════════════════════
// NOTIFICATIONS
// ═══════════════════════════════════════════════════════════════════════════════

// NotifySignal sends a signal alert
func (b *TelegramBot) NotifySignal(asset, side string, entry, tp, sl decimal.Decimal, reason string) {
	emoji := "🎯"
	if side == "YES" {
		emoji = "🟢"
	} else {
		emoji = "🔴"
	}

	msg := fmt.Sprintf(`%s *SIGNAL DETECTED*

📊 *%s* — %s
━━━━━━━━━━━━━━━━
💵 Entry: *%s¢*
🎯 TP: *%s¢* (+%s¢)
🛑 SL: *%s¢* (-%s¢)
━━━━━━━━━━━━━━━━
📝 %s`,
		emoji,
		asset, side,
		entry.Mul(decimal.NewFromInt(100)).StringFixed(1),
		tp.Mul(decimal.NewFromInt(100)).StringFixed(1),
		tp.Sub(entry).Mul(decimal.NewFromInt(100)).StringFixed(1),
		sl.Mul(decimal.NewFromInt(100)).StringFixed(1),
		entry.Sub(sl).Mul(decimal.NewFromInt(100)).StringFixed(1),
		reason,
	)

	b.sendMarkdown(msg)
}

// NotifyTrade sends a trade execution alert
func (b *TelegramBot) NotifyTrade(action, asset, side string, price, size decimal.Decimal) {
	var emoji string
	switch action {
	case "OPEN":
		emoji = "✅"
	case "CLOSE":
		emoji = "📊"
	case "TAKE_PROFIT":
		emoji = "💰"
	case "STOP_LOSS":
		emoji = "🛑"
	default:
		emoji = "📌"
	}

	msg := fmt.Sprintf(`%s *%s*

📊 %s %s
💵 Price: *%s¢*
📦 Size: *$%s*`,
		emoji, action,
		asset, side,
		price.Mul(decimal.NewFromInt(100)).StringFixed(1),
		size.StringFixed(2),
	)

	b.sendMarkdown(msg)
}

// NotifyPnL sends a P&L notification
func (b *TelegramBot) NotifyPnL(asset string, pnl decimal.Decimal, isWin bool) {
	emoji := "📈"
	if !isWin {
		emoji = "📉"
	}

	sign := "+"
	if pnl.IsNegative() {
		sign = ""
	}

	msg := fmt.Sprintf(`%s *TRADE CLOSED*

📊 %s
💵 P&L: *%s$%s*`,
		emoji, asset,
		sign, pnl.StringFixed(2),
	)

	b.sendMarkdown(msg)
}

// NotifyDailySummary sends end-of-day summary
func (b *TelegramBot) NotifyDailySummary() {
	if b.statsProvider == nil {
		return
	}

	trades, wins, losses, pnl, equity := b.statsProvider.GetStats()

	winRate := float64(0)
	if trades > 0 {
		winRate = float64(wins) / float64(trades) * 100
	}

	emoji := "📈"
	if pnl.IsNegative() {
		emoji = "📉"
	}

	sign := "+"
	if pnl.IsNegative() {
		sign = ""
	}

	msg := fmt.Sprintf(`%s *DAILY SUMMARY*
━━━━━━━━━━━━━━━━━━━━

📊 Trades: *%d*
✅ Wins: *%d*
❌ Losses: *%d*
📈 Win Rate: *%.1f%%*

━━━━━━━━━━━━━━━━━━━━
💵 P&L: *%s$%s*
💰 Equity: *$%s*`,
		emoji,
		trades, wins, losses, winRate,
		sign, pnl.StringFixed(2),
		equity.StringFixed(2),
	)

	b.sendMarkdown(msg)
}

// NotifyError sends an error alert
func (b *TelegramBot) NotifyError(err error) {
	msg := fmt.Sprintf("⚠️ *ERROR*\n\n`%s`", err.Error())
	b.sendMarkdown(msg)
}

// NotifyStartup sends startup notification
func (b *TelegramBot) NotifyStartup(mode string) {
	msg := fmt.Sprintf(`🚀 *POLYBOT STARTED*
━━━━━━━━━━━━━━━━━━━━

🎯 Strategy: *Sniper V3*
📊 Mode: *%s*
⏱️ Detection: *200ms*

━━━━━━━━━━━━━━━━━━━━
Entry: 88-93¢ | TP: 99¢ | SL: 70¢
Time: Last 15-60 seconds

Use /help for commands`, mode)

	b.sendMarkdown(msg)
}

// ═══════════════════════════════════════════════════════════════════════════════
// COMMAND HANDLING
// ═══════════════════════════════════════════════════════════════════════════════

func (b *TelegramBot) commandLoop() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-b.stopCh:
			return
		case update := <-updates:
			if update.Message == nil || !update.Message.IsCommand() {
				continue
			}

			// Only respond to authorized chat
			if update.Message.Chat.ID != b.chatID {
				continue
			}

			b.handleCommand(update.Message)
		}
	}
}

func (b *TelegramBot) handleCommand(msg *tgbotapi.Message) {
	cmd := strings.ToLower(msg.Command())

	switch cmd {
	case "start", "help":
		b.cmdHelp()
	case "status":
		b.cmdStatus()
	case "stats":
		b.cmdStats()
	case "positions":
		b.cmdPositions()
	case "pause":
		b.cmdPause()
	case "resume":
		b.cmdResume()
	case "ping":
		b.send("🏓 Pong!")
	default:
		b.send("❓ Unknown command. Use /help")
	}
}

func (b *TelegramBot) cmdHelp() {
	msg := `🤖 *POLYBOT COMMANDS*
━━━━━━━━━━━━━━━━━━━━

📊 /status — Bot status
📈 /stats — Trading statistics
💼 /positions — Open positions
⏸️ /pause — Pause trading
▶️ /resume — Resume trading
🏓 /ping — Test connection

━━━━━━━━━━━━━━━━━━━━
Sniper V3 — Last minute signals`

	b.sendMarkdown(msg)
}

func (b *TelegramBot) cmdStatus() {
	mode := "LIVE"
	if os.Getenv("DRY_RUN") == "true" {
		mode = "PAPER"
	}

	status := "🟢 RUNNING"
	// Could add pause state here

	msg := fmt.Sprintf(`📊 *BOT STATUS*
━━━━━━━━━━━━━━━━━━━━

%s
📊 Mode: *%s*
🎯 Strategy: *Sniper V3*
⏱️ Detection: *200ms*

Entry: 88-93¢ | TP: 99¢ | SL: 70¢`, status, mode)

	b.sendMarkdown(msg)
}

func (b *TelegramBot) cmdStats() {
	if b.statsProvider == nil {
		b.send("❌ Stats not available")
		return
	}

	trades, wins, losses, pnl, equity := b.statsProvider.GetStats()

	winRate := float64(0)
	if trades > 0 {
		winRate = float64(wins) / float64(trades) * 100
	}

	sign := "+"
	if pnl.IsNegative() {
		sign = ""
	}

	msg := fmt.Sprintf(`📈 *TRADING STATS*
━━━━━━━━━━━━━━━━━━━━

📊 Total Trades: *%d*
✅ Wins: *%d*
❌ Losses: *%d*
📈 Win Rate: *%.1f%%*

━━━━━━━━━━━━━━━━━━━━
💵 Total P&L: *%s$%s*
💰 Equity: *$%s*`,
		trades, wins, losses, winRate,
		sign, pnl.StringFixed(2),
		equity.StringFixed(2),
	)

	b.sendMarkdown(msg)
}

func (b *TelegramBot) cmdPositions() {
	// Positions feature requires extended interface
	b.send("📭 Position tracking available in next update")
}

func (b *TelegramBot) cmdPause() {
	b.mu.RLock()
	cb := b.onPause
	b.mu.RUnlock()

	if cb != nil {
		cb()
	}

	b.send("⏸️ Trading paused")
	log.Info().Msg("Trading paused via Telegram")
}

func (b *TelegramBot) cmdResume() {
	b.mu.RLock()
	cb := b.onResume
	b.mu.RUnlock()

	if cb != nil {
		cb()
	}

	b.send("▶️ Trading resumed")
	log.Info().Msg("Trading resumed via Telegram")
}

// ═══════════════════════════════════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════════════════════════════════

func (b *TelegramBot) send(text string) {
	msg := tgbotapi.NewMessage(b.chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		log.Error().Err(err).Msg("Failed to send Telegram message")
	}
}

func (b *TelegramBot) sendMarkdown(text string) {
	msg := tgbotapi.NewMessage(b.chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := b.api.Send(msg); err != nil {
		log.Error().Err(err).Msg("Failed to send Telegram message")
	}
}

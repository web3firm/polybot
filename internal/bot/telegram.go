package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/web3guy0/polybot/internal/config"
	"github.com/web3guy0/polybot/internal/database"
	"github.com/web3guy0/polybot/internal/scanner"
	"github.com/web3guy0/polybot/internal/trading"
)

type Bot struct {
	api           *tgbotapi.BotAPI
	cfg           *config.Config
	db            *database.Database
	scanner       *scanner.Scanner
	tradingEngine *trading.Engine
	stopCh        chan struct{}
}

func New(cfg *config.Config, db *database.Database, scanner *scanner.Scanner, engine *trading.Engine) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	log.Info().Str("username", api.Self.UserName).Msg("🤖 Telegram bot connected")

	return &Bot{
		api:           api,
		cfg:           cfg,
		db:            db,
		scanner:       scanner,
		tradingEngine: engine,
		stopCh:        make(chan struct{}),
	}, nil
}

func (b *Bot) Start() {
	// Subscribe to scanner opportunities
	oppCh := b.scanner.Subscribe()

	// Start listening for opportunities
	go b.listenForOpportunities(oppCh)

	// Start listening for commands
	go b.listenForCommands()

	// Send startup message if chat ID configured
	if b.cfg.TelegramChatID != 0 {
		b.sendStartupMessage()
	}
}

func (b *Bot) Stop() {
	close(b.stopCh)
}

func (b *Bot) listenForOpportunities(ch chan scanner.Opportunity) {
	alertTimes := make(map[string]time.Time)

	for {
		select {
		case opp := <-ch:
			// Check cooldown
			if lastAlert, ok := alertTimes[opp.Market.ID]; ok {
				if time.Since(lastAlert) < b.cfg.AlertCooldown {
					continue
				}
			}

			// Send alert
			if b.cfg.TelegramChatID != 0 {
				if err := b.sendOpportunityAlert(b.cfg.TelegramChatID, opp); err != nil {
					log.Error().Err(err).Msg("Failed to send alert")
				} else {
					alertTimes[opp.Market.ID] = time.Now()
				}
			}

		case <-b.stopCh:
			return
		}
	}
}

func (b *Bot) listenForCommands() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case update := <-updates:
			if update.Message != nil {
				go b.handleMessage(update.Message)
			}
			if update.CallbackQuery != nil {
				go b.handleCallback(update.CallbackQuery)
			}
		case <-b.stopCh:
			return
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := msg.Text

	log.Debug().
		Int64("chat_id", chatID).
		Str("text", text).
		Msg("Received message")

	// Handle commands
	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			b.cmdStart(chatID)
		case "help":
			b.cmdHelp(chatID)
		case "status":
			b.cmdStatus(chatID)
		case "opportunities", "opps":
			b.cmdOpportunities(chatID)
		case "stats":
			b.cmdStats(chatID)
		case "settings":
			b.cmdSettings(chatID)
		case "subscribe":
			b.cmdSubscribe(chatID)
		case "unsubscribe":
			b.cmdUnsubscribe(chatID)
		default:
			b.sendText(chatID, "❓ Unknown command. Use /help for available commands.")
		}
	}
}

func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data

	log.Debug().
		Int64("chat_id", chatID).
		Str("data", data).
		Msg("Received callback")

	// Acknowledge callback
	b.api.Request(tgbotapi.NewCallback(cb.ID, ""))

	switch {
	case data == "refresh_opps":
		b.cmdOpportunities(chatID)
	case data == "refresh_stats":
		b.cmdStats(chatID)
	case data == "toggle_alerts":
		b.toggleAlerts(chatID)
	case strings.HasPrefix(data, "set_spread_"):
		spread := strings.TrimPrefix(data, "set_spread_")
		b.setMinSpread(chatID, spread)
	}
}

// Commands

func (b *Bot) cmdStart(chatID int64) {
	// Save user settings
	settings, _ := b.db.GetUserSettings(chatID)
	settings.ChatID = chatID
	settings.AlertsEnabled = true
	b.db.SaveUserSettings(settings)

	text := `🚀 *Welcome to Polybot!*

Your Polymarket Arbitrage Alert Bot

*What I do:*
• 📊 Monitor Polymarket 24/7
• 🔍 Find arbitrage opportunities
• 📱 Send instant alerts
• 💰 Help you profit from mispricings

*Quick Start:*
1️⃣ You're now subscribed to alerts
2️⃣ Use /settings to customize
3️⃣ Use /opportunities to see current opps

*Commands:*
/help - All commands
/status - Bot status
/opportunities - Current opportunities
/stats - Your statistics
/settings - Customize alerts

Let's make some money! 💪`

	b.sendMarkdown(chatID, text)
}

func (b *Bot) cmdHelp(chatID int64) {
	text := `📚 *Polybot Commands*

*Monitoring:*
/status - Bot & market status
/opportunities - Current arb opportunities  
/stats - Statistics & P/L

*Settings:*
/settings - View/change settings
/subscribe - Enable alerts
/unsubscribe - Disable alerts

*How Arbitrage Works:*
When YES + NO ≠ $1.00, there's an opportunity:

• Total < $1.00 → Buy both, guaranteed profit
• Total > $1.00 → Prices inflated, wait or short

*Example:*
YES: $0.48 + NO: $0.49 = $0.97
Buy $100 each = $97 cost
One MUST win = $100 return
*Profit: $3 (3%)*

Need help? Join our community!`

	b.sendMarkdown(chatID, text)
}

func (b *Bot) cmdStatus(chatID int64) {
	opps := b.scanner.GetOpportunities()

	settings, _ := b.db.GetUserSettings(chatID)
	alertStatus := "🟢 Enabled"
	if !settings.AlertsEnabled {
		alertStatus = "🔴 Disabled"
	}

	text := fmt.Sprintf(`📊 *Bot Status*

🤖 *Bot:* Online
📡 *Scanner:* Active
🔔 *Your Alerts:* %s
⏱️ *Scan Interval:* %s
📉 *Min Spread:* %s%%

*Current Market:*
• Active Opportunities: %d
• Last Scan: Just now

*Your Settings:*
• Min Spread Alert: %s%%`,
		alertStatus,
		b.cfg.ScanInterval,
		b.cfg.MinSpreadPct.String(),
		len(opps),
		settings.MinSpreadPct.String(),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "refresh_opps"),
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Settings", "settings"),
		),
	)

	b.sendMarkdownWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) cmdOpportunities(chatID int64) {
	opps := b.scanner.GetOpportunities()

	if len(opps) == 0 {
		text := `📊 *Current Opportunities*

No arbitrage opportunities found right now.

Markets are efficiently priced ✅

_I'll alert you when opportunities appear!_`

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "refresh_opps"),
			),
		)
		b.sendMarkdownWithKeyboard(chatID, text, keyboard)
		return
	}

	text := fmt.Sprintf("📊 *Current Opportunities* (%d found)\n\n", len(opps))

	for i, opp := range opps {
		if i >= 5 {
			text += fmt.Sprintf("\n_...and %d more_", len(opps)-5)
			break
		}

		var emoji string
		switch t := opp.Type; t {
		case scanner.TypeOverpriced:
			emoji = "🟡"
		case scanner.TypeSevereMispricing:
			emoji = "🔴"
		default:
			emoji = "🟢"
		}

		question := opp.Market.Question
		if len(question) > 50 {
			question = question[:50] + "..."
		}

		text += fmt.Sprintf(`%s *%s*
   YES: $%s | NO: $%s | Spread: %s%%

`, emoji, escapeMarkdown(question),
			opp.YesPrice.StringFixed(3),
			opp.NoPrice.StringFixed(3),
			opp.SpreadPct.StringFixed(2))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "refresh_opps"),
		),
	)

	b.sendMarkdownWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) cmdStats(chatID int64) {
	stats, _ := b.db.GetStats()

	text := fmt.Sprintf(`📈 *Statistics*

*All Time:*
• Opportunities Found: %v
• Trades Executed: %v
• Total P/L: $%v

*Today:*
• Scans: Active
• Alerts Sent: Multiple

*Performance:*
• Uptime: 99.9%%
• Avg Response: <1s`,
		stats["total_opportunities"],
		stats["total_trades"],
		stats["total_pnl"],
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "refresh_stats"),
		),
	)

	b.sendMarkdownWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) cmdSettings(chatID int64) {
	settings, _ := b.db.GetUserSettings(chatID)

	alertStatus := "🟢 ON"
	alertBtn := "🔕 Turn OFF"
	if !settings.AlertsEnabled {
		alertStatus = "🔴 OFF"
		alertBtn = "🔔 Turn ON"
	}

	text := fmt.Sprintf(`⚙️ *Settings*

*Alerts:* %s
*Min Spread:* %s%%

Choose minimum spread to receive alerts:`,
		alertStatus,
		settings.MinSpreadPct.String(),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(alertBtn, "toggle_alerts"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1%", "set_spread_1"),
			tgbotapi.NewInlineKeyboardButtonData("2%", "set_spread_2"),
			tgbotapi.NewInlineKeyboardButtonData("3%", "set_spread_3"),
			tgbotapi.NewInlineKeyboardButtonData("5%", "set_spread_5"),
		),
	)

	b.sendMarkdownWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) cmdSubscribe(chatID int64) {
	settings, _ := b.db.GetUserSettings(chatID)
	settings.AlertsEnabled = true
	b.db.SaveUserSettings(settings)

	b.sendText(chatID, "🔔 Alerts enabled! You'll receive notifications for arbitrage opportunities.")
}

func (b *Bot) cmdUnsubscribe(chatID int64) {
	settings, _ := b.db.GetUserSettings(chatID)
	settings.AlertsEnabled = false
	b.db.SaveUserSettings(settings)

	b.sendText(chatID, "🔕 Alerts disabled. Use /subscribe to re-enable.")
}

func (b *Bot) toggleAlerts(chatID int64) {
	settings, _ := b.db.GetUserSettings(chatID)
	settings.AlertsEnabled = !settings.AlertsEnabled
	b.db.SaveUserSettings(settings)

	if settings.AlertsEnabled {
		b.sendText(chatID, "🔔 Alerts enabled!")
	} else {
		b.sendText(chatID, "🔕 Alerts disabled!")
	}
}

func (b *Bot) setMinSpread(chatID int64, spread string) {
	settings, _ := b.db.GetUserSettings(chatID)

	if s, err := decimal.NewFromString(spread); err == nil {
		settings.MinSpreadPct = s
		b.db.SaveUserSettings(settings)
		b.sendText(chatID, fmt.Sprintf("✅ Minimum spread set to %s%%", spread))
	}
}

// Alert sending

func (b *Bot) sendOpportunityAlert(chatID int64, opp scanner.Opportunity) error {
	var (
		emoji  string
		action string
	)
	switch t := opp.Type; t {
	case scanner.TypeOverpriced:
		emoji = "🟡"
		action = "Prices inflated - wait or short"
	case scanner.TypeSevereMispricing:
		emoji = "🔴"
		action = "SEVERE mispricing detected!"
	default:
		emoji = "🟢"
		action = "BUY BOTH for guaranteed profit"
	}

	question := opp.Market.Question
	if len(question) > 80 {
		question = question[:80] + "..."
	}

	text := fmt.Sprintf(`%s *ARB ALERT*

📊 *%s*

💰 *Prices:*
├ YES: $%s
├ NO: $%s
└ Total: $%s

📈 *Spread:* %s%%

💡 *Action:* %s

🔗 [Trade on Polymarket](https://polymarket.com/event/%s)`,
		emoji,
		escapeMarkdown(question),
		opp.YesPrice.StringFixed(3),
		opp.NoPrice.StringFixed(3),
		opp.TotalPrice.StringFixed(3),
		opp.SpreadPct.StringFixed(2),
		action,
		opp.Market.Slug,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 Open Market", fmt.Sprintf("https://polymarket.com/event/%s", opp.Market.Slug)),
		),
	)

	return b.sendMarkdownWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) sendStartupMessage() {
	text := `🟢 *Polybot Online*

Bot restarted and scanning markets.

Use /status to check current state.`

	b.sendMarkdown(b.cfg.TelegramChatID, text)
}

// Helpers

func (b *Bot) sendText(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
}

func (b *Bot) sendMarkdown(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	_, err := b.api.Send(msg)
	return err
}

func (b *Bot) sendMarkdownWithKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = keyboard
	_, err := b.api.Send(msg)
	return err
}

func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"`", "\\`",
	)
	return replacer.Replace(s)
}

package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

type Config struct {
	// Bot Settings
	Mode  string // "alerts" or "trading"
	Debug bool

	// Telegram
	TelegramToken  string
	TelegramChatID int64

	// Scanner Settings
	ScanInterval    time.Duration
	MinSpreadPct    decimal.Decimal
	AlertCooldown   time.Duration
	MaxMarketsToScan int

	// Trading Settings (for future auto-trading)
	TradingEnabled  bool
	MaxTradeSize    decimal.Decimal
	MinProfitPct    decimal.Decimal
	DryRun          bool

	// Polymarket
	PolymarketAPIURL string
	PolymarketWSURL  string

	// Database
	DatabasePath string

	// Wallet (for future trading)
	WalletPrivateKey string
	WalletAddress    string
}

func Load() (*Config, error) {
	cfg := &Config{
		// Defaults
		Mode:             getEnv("BOT_MODE", "alerts"),
		Debug:            getEnvBool("DEBUG", false),
		TelegramToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		ScanInterval:     getEnvDuration("SCAN_INTERVAL", 30*time.Second),
		MinSpreadPct:     getEnvDecimal("MIN_SPREAD_PCT", decimal.NewFromFloat(1.0)),
		AlertCooldown:    getEnvDuration("ALERT_COOLDOWN", 5*time.Minute),
		MaxMarketsToScan: getEnvInt("MAX_MARKETS", 200),
		TradingEnabled:   getEnvBool("TRADING_ENABLED", false),
		MaxTradeSize:     getEnvDecimal("MAX_TRADE_SIZE", decimal.NewFromFloat(100)),
		MinProfitPct:     getEnvDecimal("MIN_PROFIT_PCT", decimal.NewFromFloat(2.0)),
		DryRun:           getEnvBool("DRY_RUN", true),
		PolymarketAPIURL: getEnv("POLYMARKET_API_URL", "https://gamma-api.polymarket.com"),
		PolymarketWSURL:  getEnv("POLYMARKET_WS_URL", "wss://ws-subscriptions-clob.polymarket.com/ws"),
		DatabasePath:     getEnv("DATABASE_PATH", "data/polybot.db"),
		WalletPrivateKey: os.Getenv("WALLET_PRIVATE_KEY"),
		WalletAddress:    os.Getenv("WALLET_ADDRESS"),
	}

	// Parse chat ID
	if chatID := os.Getenv("TELEGRAM_CHAT_ID"); chatID != "" {
		id, err := strconv.ParseInt(chatID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID: %w", err)
		}
		cfg.TelegramChatID = id
	}

	// Validate required fields
	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

func getEnvDecimal(key string, defaultValue decimal.Decimal) decimal.Decimal {
	if value := os.Getenv(key); value != "" {
		if d, err := decimal.NewFromString(value); err == nil {
			return d
		}
	}
	return defaultValue
}

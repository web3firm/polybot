package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/web3guy0/polybot/bot"
	"github.com/web3guy0/polybot/core"
	"github.com/web3guy0/polybot/exec"
	"github.com/web3guy0/polybot/feeds"
	"github.com/web3guy0/polybot/risk"
	"github.com/web3guy0/polybot/storage"
	"github.com/web3guy0/polybot/strategy"
)

func main() {
	// ═══════════════════════════════════════════════════════════════════════════════
	// BOOTSTRAP
	// ═══════════════════════════════════════════════════════════════════════════════

	// Load environment
	if err := godotenv.Load(); err != nil {
		log.Warn().Msg("No .env file found")
	}

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})

	if os.Getenv("DEBUG") == "true" {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Info().Msg("═══════════════════════════════════════════════════════════════")
	log.Info().Msg("                    POLYBOT v6.0 - SNIPER")
	log.Info().Msg("═══════════════════════════════════════════════════════════════")

	// ═══════════════════════════════════════════════════════════════════════════════
	// INITIALIZE COMPONENTS
	// ═══════════════════════════════════════════════════════════════════════════════

	// 1. Storage (for state persistence)
	db, err := storage.NewDatabase()
	if err != nil {
		log.Warn().Err(err).Msg("Database connection failed, continuing without persistence")
	} else {
		log.Info().Msg("✅ Storage layer initialized")
	}

	// 2. Binance feed (for real-time crypto prices)
	binanceFeed := feeds.NewBinanceFeed()
	binanceFeed.Start()
	log.Info().Msg("✅ Binance price feed initialized")

	// 3. Polymarket feeds
	polyFeed := feeds.NewPolymarketFeed()
	log.Info().Msg("✅ Polymarket feed initialized")

	// 4. Window Scanner (tracks 15-min crypto windows)
	windowScanner := feeds.NewWindowScanner(binanceFeed)
	windowScanner.Start()
	log.Info().Msg("✅ Window scanner initialized")

	// 5. Execution client
	executor, err := exec.NewClient()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize executor")
	}
	log.Info().Msg("✅ Execution layer initialized")

	// 6. Risk manager
	riskMgr := risk.NewManager()
	log.Info().Msg("✅ Risk layer initialized")

	// 7. Sniper strategy
	sniper := strategy.NewSniper(binanceFeed, windowScanner)
	strategies := []strategy.Strategy{sniper}
	log.Info().Msg("✅ Strategy loaded")

	// 8. Core engine
	engine := core.NewEngine(polyFeed, executor, riskMgr, strategies, db)
	log.Info().Msg("✅ Engine initialized")

	// 9. Telegram bot (optional - fails gracefully if not configured)
	var tgBot *bot.TelegramBot
	if tg, err := bot.NewTelegramBot(engine); err != nil {
		log.Warn().Err(err).Msg("Telegram bot not available")
	} else {
		tgBot = tg
		tgBot.Start()
		log.Info().Msg("✅ Telegram initialized")
	}

	// ═══════════════════════════════════════════════════════════════════════════════
	// STATUS
	// ═══════════════════════════════════════════════════════════════════════════════

	mode := "LIVE"
	if os.Getenv("DRY_RUN") == "true" {
		mode = "PAPER"
	}

	log.Info().Msg("")
	log.Info().Msg("╔═══════════════════════════════════════╗")
	log.Info().Msg("║           POLYBOT SNIPER              ║")
	log.Info().Msg("╠═══════════════════════════════════════╣")
	log.Info().Msgf("║  Mode:    %-27s ║", mode)
	log.Info().Msg("║  Assets:  BTC, ETH, SOL               ║")
	log.Info().Msg("║  Scan:    100ms                       ║")
	log.Info().Msg("║  Entry:   88-93¢                      ║")
	log.Info().Msg("║  TP/SL:   99¢ / 70¢                   ║")
	log.Info().Msg("║  Window:  15-60 sec                   ║")
	log.Info().Msg("╚═══════════════════════════════════════╝")
	log.Info().Msg("")

	// ═══════════════════════════════════════════════════════════════════════════════
	// START
	// ═══════════════════════════════════════════════════════════════════════════════

	// Start engine
	go engine.Start()

	// Start sniper's fast scan loop
	signalCh := make(chan *strategy.Signal, 100)
	go sniper.RunLoop(signalCh)

	// Process signals
	go func() {
		for sig := range signalCh {
			engine.ProcessSignal(sig, sniper.Name())
		}
	}()

	log.Info().Msg("🚀 Running...")

	// Telegram startup
	if tgBot != nil {
		mode := "PAPER"
		if os.Getenv("DRY_RUN") != "true" {
			mode = "LIVE"
		}
		tgBot.NotifyStartup(mode)
	}

	// ═══════════════════════════════════════════════════════════════════════════════
	// GRACEFUL SHUTDOWN
	// ═══════════════════════════════════════════════════════════════════════════════

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info().Msg("🛑 Shutting down...")
	engine.Stop()
	binanceFeed.Stop()
	windowScanner.Stop()

	if tgBot != nil {
		tgBot.Stop()
	}

	if db != nil {
		db.Close()
	}

	log.Info().Msg("👋 Goodbye!")
}

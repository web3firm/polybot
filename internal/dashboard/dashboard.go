package dashboard

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/shopspring/decimal"
)

// Dashboard provides a live-updating terminal UI for the trading bot
type Dashboard struct {
	mu sync.RWMutex

	// State
	running   bool
	startTime time.Time

	// Positions
	positions map[string]*Position

	// Recent trades
	recentTrades []TradeLog

	// Stats
	totalTrades   int
	winningTrades int
	totalProfit   decimal.Decimal
	balance       decimal.Decimal

	// Logs buffer
	logs      []string
	maxLogs   int

	// Price data
	prices map[string]PriceInfo

	// Update channel
	updateCh chan struct{}
	stopCh   chan struct{}
}

type Position struct {
	Asset      string
	Side       string
	EntryPrice decimal.Decimal
	CurrentPrice decimal.Decimal
	Size       int64
	PnL        decimal.Decimal
	PnLPct     decimal.Decimal
	HoldTime   time.Duration
	Status     string // "OPEN", "SELLING", "COOLDOWN"
}

type TradeLog struct {
	Time      time.Time
	Asset     string
	Action    string // "BUY", "SELL", "STOP"
	Price     decimal.Decimal
	Size      int64
	PnL       decimal.Decimal
	Result    string // "✅", "❌", "⏳"
}

type PriceInfo struct {
	Asset     string
	Binance   decimal.Decimal
	Chainlink decimal.Decimal
	UpOdds    decimal.Decimal
	DownOdds  decimal.Decimal
	Updated   time.Time
}

// New creates a new dashboard
func New() *Dashboard {
	return &Dashboard{
		positions:    make(map[string]*Position),
		recentTrades: make([]TradeLog, 0),
		logs:         make([]string, 0),
		maxLogs:      10,
		prices:       make(map[string]PriceInfo),
		totalProfit:  decimal.Zero,
		balance:      decimal.Zero,
		updateCh:     make(chan struct{}, 100),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the dashboard refresh loop
func (d *Dashboard) Start() {
	d.running = true
	d.startTime = time.Now()
	go d.refreshLoop()
}

// Stop stops the dashboard
func (d *Dashboard) Stop() {
	d.running = false
	close(d.stopCh)
}

// UpdatePosition updates a position in the dashboard
func (d *Dashboard) UpdatePosition(asset, side string, entry, current decimal.Decimal, size int64, status string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	pnl := current.Sub(entry).Mul(decimal.NewFromInt(size))
	pnlPct := decimal.Zero
	if !entry.IsZero() {
		pnlPct = current.Sub(entry).Div(entry).Mul(decimal.NewFromInt(100))
	}

	d.positions[asset] = &Position{
		Asset:        asset,
		Side:         side,
		EntryPrice:   entry,
		CurrentPrice: current,
		Size:         size,
		PnL:          pnl,
		PnLPct:       pnlPct,
		Status:       status,
	}

	d.triggerUpdate()
}

// RemovePosition removes a position
func (d *Dashboard) RemovePosition(asset string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.positions, asset)
	d.triggerUpdate()
}

// AddTrade adds a trade to the log
func (d *Dashboard) AddTrade(asset, action string, price decimal.Decimal, size int64, pnl decimal.Decimal, result string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	trade := TradeLog{
		Time:   time.Now(),
		Asset:  asset,
		Action: action,
		Price:  price,
		Size:   size,
		PnL:    pnl,
		Result: result,
	}

	d.recentTrades = append([]TradeLog{trade}, d.recentTrades...)
	if len(d.recentTrades) > 5 {
		d.recentTrades = d.recentTrades[:5]
	}

	d.triggerUpdate()
}

// UpdateStats updates overall stats
func (d *Dashboard) UpdateStats(totalTrades, winningTrades int, totalProfit, balance decimal.Decimal) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.totalTrades = totalTrades
	d.winningTrades = winningTrades
	d.totalProfit = totalProfit
	d.balance = balance
	d.triggerUpdate()
}

// UpdatePrice updates price info for an asset
func (d *Dashboard) UpdatePrice(asset string, binance, chainlink, upOdds, downOdds decimal.Decimal) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prices[asset] = PriceInfo{
		Asset:     asset,
		Binance:   binance,
		Chainlink: chainlink,
		UpOdds:    upOdds,
		DownOdds:  downOdds,
		Updated:   time.Now(),
	}
	d.triggerUpdate()
}

// AddLog adds a log message
func (d *Dashboard) AddLog(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	d.logs = append(d.logs, fmt.Sprintf("[%s] %s", timestamp, msg))
	if len(d.logs) > d.maxLogs {
		d.logs = d.logs[len(d.logs)-d.maxLogs:]
	}
	d.triggerUpdate()
}

func (d *Dashboard) triggerUpdate() {
	select {
	case d.updateCh <- struct{}{}:
	default:
	}
}

func (d *Dashboard) refreshLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.render()
		case <-d.updateCh:
			d.render()
		}
	}
}

func (d *Dashboard) render() {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Clear screen
	fmt.Print("\033[H\033[2J")

	// Colors
	headerColor := color.New(color.FgCyan, color.Bold)
	greenColor := color.New(color.FgGreen)
	redColor := color.New(color.FgRed)
	yellowColor := color.New(color.FgYellow)
	
	// Header
	uptime := time.Since(d.startTime).Round(time.Second)
	winRate := 0.0
	if d.totalTrades > 0 {
		winRate = float64(d.winningTrades) / float64(d.totalTrades) * 100
	}

	headerColor.Println("╔══════════════════════════════════════════════════════════════════════════════╗")
	headerColor.Printf("║  🤖 POLYBOT ML SCALPER                                                       ║\n")
	headerColor.Printf("║  Uptime: %-10s | Trades: %-3d | Win Rate: %5.1f%% | P&L: %-10s     ║\n",
		uptime, d.totalTrades, winRate, d.formatPnL(d.totalProfit))
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")

	// Prices Table
	headerColor.Println("║  📊 MARKET PRICES                                                            ║")
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")
	
	if len(d.prices) > 0 {
		fmt.Printf("║  %-6s │ %-12s │ %-12s │ %-8s │ %-8s │ %-6s        ║\n",
			"Asset", "Binance", "Chainlink", "UP", "DOWN", "Cheap")
		fmt.Println("║  " + strings.Repeat("─", 72) + "  ║")
		
		for _, p := range d.prices {
			cheap := ""
			if p.UpOdds.LessThan(decimal.NewFromFloat(0.15)) {
				cheap = "UP ⬆️"
			} else if p.DownOdds.LessThan(decimal.NewFromFloat(0.15)) {
				cheap = "DOWN ⬇️"
			}
			
			fmt.Printf("║  %-6s │ $%-11s │ $%-11s │ %-8s │ %-8s │ %-10s  ║\n",
				p.Asset,
				p.Binance.StringFixed(2),
				p.Chainlink.StringFixed(2),
				p.UpOdds.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
				p.DownOdds.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
				cheap)
		}
	} else {
		fmt.Println("║  Waiting for price data...                                                     ║")
	}

	// Positions Table
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")
	headerColor.Println("║  📈 ACTIVE POSITIONS                                                         ║")
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")

	if len(d.positions) > 0 {
		fmt.Printf("║  %-5s │ %-4s │ %-7s │ %-7s │ %-5s │ %-10s │ %-8s │ %-8s ║\n",
			"Asset", "Side", "Entry", "Current", "Size", "P&L", "P&L %", "Status")
		fmt.Println("║  " + strings.Repeat("─", 72) + "  ║")

		for _, pos := range d.positions {
			pnlStr := d.formatPnL(pos.PnL)
			pnlPctStr := pos.PnLPct.StringFixed(1) + "%"
			
			statusIcon := "🟢"
			if pos.Status == "SELLING" {
				statusIcon = "🔄"
			} else if pos.Status == "COOLDOWN" {
				statusIcon = "⏳"
			}

			if pos.PnL.GreaterThan(decimal.Zero) {
				greenColor.Printf("║  %-5s │ %-4s │ %-7s │ %-7s │ %-5d │ %-10s │ %-8s │ %s %-6s ║\n",
					pos.Asset, pos.Side,
					pos.EntryPrice.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
					pos.CurrentPrice.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
					pos.Size, pnlStr, pnlPctStr, statusIcon, pos.Status)
			} else {
				redColor.Printf("║  %-5s │ %-4s │ %-7s │ %-7s │ %-5d │ %-10s │ %-8s │ %s %-6s ║\n",
					pos.Asset, pos.Side,
					pos.EntryPrice.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
					pos.CurrentPrice.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
					pos.Size, pnlStr, pnlPctStr, statusIcon, pos.Status)
			}
		}
	} else {
		yellowColor.Println("║  No active positions - scanning for opportunities...                         ║")
	}

	// Recent Trades
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")
	headerColor.Println("║  📜 RECENT TRADES                                                            ║")
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")

	if len(d.recentTrades) > 0 {
		for _, trade := range d.recentTrades {
			pnlStr := ""
			if trade.Action == "SELL" || trade.Action == "STOP" {
				pnlStr = d.formatPnL(trade.PnL)
			}
			
			actionColor := greenColor
			if trade.Action == "SELL" {
				actionColor = yellowColor
			} else if trade.Action == "STOP" {
				actionColor = redColor
			}
			
			actionColor.Printf("║  %s [%s] %-4s %-5s @ %-5s x%-4d %s %-15s                  ║\n",
				trade.Result,
				trade.Time.Format("15:04:05"),
				trade.Action,
				trade.Asset,
				trade.Price.Mul(decimal.NewFromInt(100)).StringFixed(0)+"¢",
				trade.Size,
				pnlStr,
				"")
		}
	} else {
		fmt.Println("║  No trades yet...                                                              ║")
	}

	// Logs
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")
	headerColor.Println("║  📋 ACTIVITY LOG                                                             ║")
	headerColor.Println("╠══════════════════════════════════════════════════════════════════════════════╣")

	if len(d.logs) > 0 {
		for _, log := range d.logs {
			// Truncate if too long
			if len(log) > 74 {
				log = log[:71] + "..."
			}
			fmt.Printf("║  %-74s  ║\n", log)
		}
	} else {
		fmt.Println("║  Waiting for activity...                                                       ║")
	}

	headerColor.Println("╚══════════════════════════════════════════════════════════════════════════════╝")
	
	// Footer
	fmt.Printf("\nPress Ctrl+C to stop | Balance: $%.2f\n", d.balance.InexactFloat64())
}

func (d *Dashboard) formatPnL(pnl decimal.Decimal) string {
	if pnl.GreaterThanOrEqual(decimal.Zero) {
		return "+$" + pnl.StringFixed(2)
	}
	return "-$" + pnl.Abs().StringFixed(2)
}

// DashboardWriter implements io.Writer for log capture
type DashboardWriter struct {
	dashboard *Dashboard
}

func (d *Dashboard) Writer() *DashboardWriter {
	return &DashboardWriter{dashboard: d}
}

func (w *DashboardWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.dashboard.AddLog(msg)
	}
	return len(p), nil
}

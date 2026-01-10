package dashboard

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/shopspring/decimal"
)

// Dashboard provides a live-updating 4-quadrant terminal UI
// ┌─────────────────────────────┬─────────────────────────────┐
// │  📊 MARKET DATA             │  📈 ACTIVE POSITIONS        │
// │  Live prices & Price2Beat   │  Open trades with P&L       │
// ├─────────────────────────────┼─────────────────────────────┤
// │  🎯 OPPORTUNITIES           │  📋 ACTIVITY LOG            │
// │  ML signals & cheap sides   │  Recent bot activity        │
// └─────────────────────────────┴─────────────────────────────┘

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
	logs    []string
	maxLogs int

	// Price data
	prices map[string]PriceInfo

	// Opportunities detected
	opportunities []Opportunity

	// Update channel
	updateCh chan struct{}
	stopCh   chan struct{}
}

type Position struct {
	Asset        string
	Side         string
	EntryPrice   decimal.Decimal
	CurrentPrice decimal.Decimal
	Size         int64
	PnL          decimal.Decimal
	PnLPct       decimal.Decimal
	HoldTime     time.Duration
	Status       string // "OPEN", "SELLING", "COOLDOWN"
	EntryTime    time.Time
}

type TradeLog struct {
	Time   time.Time
	Asset  string
	Action string // "BUY", "SELL", "STOP"
	Price  decimal.Decimal
	Size   int64
	PnL    decimal.Decimal
	Result string // "✅", "❌", "⏳"
}

type PriceInfo struct {
	Asset       string
	Binance     decimal.Decimal
	Chainlink   decimal.Decimal
	PriceToBeat decimal.Decimal
	UpOdds      decimal.Decimal
	DownOdds    decimal.Decimal
	Updated     time.Time
}

type Opportunity struct {
	Asset       string
	Side        string
	Price       decimal.Decimal
	Probability decimal.Decimal
	Signal      string // "🟢 BUY", "🟡 WAIT", "🔴 SKIP"
	Reason      string
	Time        time.Time
}

// New creates a new dashboard
func New() *Dashboard {
	return &Dashboard{
		positions:     make(map[string]*Position),
		recentTrades:  make([]TradeLog, 0),
		logs:          make([]string, 0),
		maxLogs:       8,
		prices:        make(map[string]PriceInfo),
		opportunities: make([]Opportunity, 0),
		totalProfit:   decimal.Zero,
		balance:       decimal.Zero,
		updateCh:      make(chan struct{}, 100),
		stopCh:        make(chan struct{}),
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

	existing, exists := d.positions[asset]
	entryTime := time.Now()
	if exists {
		entryTime = existing.EntryTime
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
		EntryTime:    entryTime,
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

	existing, exists := d.prices[asset]
	priceToBeat := decimal.Zero
	if exists {
		priceToBeat = existing.PriceToBeat
	}

	d.prices[asset] = PriceInfo{
		Asset:       asset,
		Binance:     binance,
		Chainlink:   chainlink,
		PriceToBeat: priceToBeat,
		UpOdds:      upOdds,
		DownOdds:    downOdds,
		Updated:     time.Now(),
	}
	d.triggerUpdate()
}

// UpdatePriceToBeat updates the price to beat for a window
func (d *Dashboard) UpdatePriceToBeat(asset string, priceToBeat decimal.Decimal) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, exists := d.prices[asset]; exists {
		existing.PriceToBeat = priceToBeat
		d.prices[asset] = existing
	}
	d.triggerUpdate()
}

// AddOpportunity adds a detected opportunity
func (d *Dashboard) AddOpportunity(asset, side string, price, probability decimal.Decimal, signal, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	opp := Opportunity{
		Asset:       asset,
		Side:        side,
		Price:       price,
		Probability: probability,
		Signal:      signal,
		Reason:      reason,
		Time:        time.Now(),
	}

	// Keep only latest per asset
	found := false
	for i, o := range d.opportunities {
		if o.Asset == asset {
			d.opportunities[i] = opp
			found = true
			break
		}
	}
	if !found {
		d.opportunities = append(d.opportunities, opp)
	}

	// Keep max 5
	if len(d.opportunities) > 5 {
		d.opportunities = d.opportunities[len(d.opportunities)-5:]
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

const (
	totalWidth = 120
	halfWidth  = 58
)

func (d *Dashboard) render() {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Clear screen
	fmt.Print("\033[H\033[2J")

	// Colors
	cyan := color.New(color.FgCyan, color.Bold)
	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)
	yellow := color.New(color.FgYellow)
	white := color.New(color.FgWhite)
	dim := color.New(color.FgHiBlack)

	// Header
	uptime := time.Since(d.startTime).Round(time.Second)
	winRate := 0.0
	if d.totalTrades > 0 {
		winRate = float64(d.winningTrades) / float64(d.totalTrades) * 100
	}

	cyan.Printf("┌")
	cyan.Printf("%s", strings.Repeat("─", totalWidth-2))
	cyan.Printf("┐\n")

	// Title bar
	title := fmt.Sprintf("  🤖 POLYBOT ML SCALPER v4.0  │  ⏱ %s  │  📊 %d trades  │  🎯 %.1f%% win  │  💰 %s  ",
		uptime, d.totalTrades, winRate, d.formatPnL(d.totalProfit))
	cyan.Printf("│")
	if d.totalProfit.GreaterThanOrEqual(decimal.Zero) {
		green.Printf("%-*s", totalWidth-2, title)
	} else {
		red.Printf("%-*s", totalWidth-2, title)
	}
	cyan.Printf("│\n")

	// Divider with split
	cyan.Printf("├")
	cyan.Printf("%s", strings.Repeat("─", halfWidth))
	cyan.Printf("┬")
	cyan.Printf("%s", strings.Repeat("─", halfWidth))
	cyan.Printf("┤\n")

	// ═══════════════════════════════════════════════════════════════════════
	// TOP ROW: Market Data (left) | Positions (right)
	// ═══════════════════════════════════════════════════════════════════════

	// Section headers
	cyan.Printf("│")
	cyan.Printf(" 📊 MARKET DATA & PRICE TO BEAT")
	cyan.Printf("%s", strings.Repeat(" ", halfWidth-31))
	cyan.Printf("│")
	cyan.Printf(" 📈 ACTIVE POSITIONS")
	cyan.Printf("%s", strings.Repeat(" ", halfWidth-21))
	cyan.Printf("│\n")

	// Prepare market data lines
	marketLines := make([]string, 0)
	marketLines = append(marketLines, fmt.Sprintf(" %-5s │ %-11s │ %-11s │ %-5s │ %-5s", "Asset", "Live Price", "Price2Beat", "UP", "DOWN"))
	marketLines = append(marketLines, " "+strings.Repeat("─", halfWidth-3))

	for _, p := range d.prices {
		priceStr := "$" + p.Binance.StringFixed(2)
		p2bStr := "$" + p.PriceToBeat.StringFixed(2)
		if p.PriceToBeat.IsZero() {
			p2bStr = "waiting..."
		}
		upStr := p.UpOdds.Mul(decimal.NewFromInt(100)).StringFixed(0) + "¢"
		downStr := p.DownOdds.Mul(decimal.NewFromInt(100)).StringFixed(0) + "¢"

		// Highlight cheap side
		if p.UpOdds.LessThan(decimal.NewFromFloat(0.20)) {
			upStr = "→" + upStr
		}
		if p.DownOdds.LessThan(decimal.NewFromFloat(0.20)) {
			downStr = "→" + downStr
		}

		marketLines = append(marketLines, fmt.Sprintf(" %-5s │ %-11s │ %-11s │ %-5s │ %-5s",
			p.Asset, priceStr, p2bStr, upStr, downStr))
	}

	if len(d.prices) == 0 {
		marketLines = append(marketLines, " Waiting for price data...")
	}

	// Prepare position lines
	posLines := make([]string, 0)
	posLines = append(posLines, fmt.Sprintf(" %-4s │ %-4s │ %-6s │ %-6s │ %-5s │ %-8s", "ASSET", "SIDE", "ENTRY", "NOW", "SIZE", "P&L"))
	posLines = append(posLines, " "+strings.Repeat("─", halfWidth-3))

	if len(d.positions) == 0 {
		posLines = append(posLines, " No positions - scanning...")
	} else {
		for _, pos := range d.positions {
			entryStr := pos.EntryPrice.Mul(decimal.NewFromInt(100)).StringFixed(0) + "¢"
			nowStr := pos.CurrentPrice.Mul(decimal.NewFromInt(100)).StringFixed(0) + "¢"
			pnlStr := d.formatPnL(pos.PnL)
			status := "🟢"
			if pos.Status == "SELLING" {
				status = "🔄"
			}
			posLines = append(posLines, fmt.Sprintf(" %s%-3s │ %-4s │ %-6s │ %-6s │ %-5d │ %-8s",
				status, pos.Asset, pos.Side, entryStr, nowStr, pos.Size, pnlStr))
		}
	}

	// Print top section (max 8 lines each)
	maxTopLines := 8
	for i := 0; i < maxTopLines; i++ {
		cyan.Printf("│")

		// Left side - market data
		if i < len(marketLines) {
			line := marketLines[i]
			if len(line) > halfWidth-1 {
				line = line[:halfWidth-1]
			}

			// Color cheap prices
			if strings.Contains(line, "→") {
				yellow.Printf("%-*s", halfWidth, line)
			} else {
				white.Printf("%-*s", halfWidth, line)
			}
		} else {
			fmt.Printf("%s", strings.Repeat(" ", halfWidth))
		}

		cyan.Printf("│")

		// Right side - positions
		if i < len(posLines) {
			line := posLines[i]
			if len(line) > halfWidth-1 {
				line = line[:halfWidth-1]
			}

			// Color by P&L
			if strings.Contains(line, "+$") {
				green.Printf("%-*s", halfWidth, line)
			} else if strings.Contains(line, "-$") {
				red.Printf("%-*s", halfWidth, line)
			} else {
				white.Printf("%-*s", halfWidth, line)
			}
		} else {
			fmt.Printf("%s", strings.Repeat(" ", halfWidth))
		}

		cyan.Printf("│\n")
	}

	// Middle divider
	cyan.Printf("├")
	cyan.Printf("%s", strings.Repeat("─", halfWidth))
	cyan.Printf("┼")
	cyan.Printf("%s", strings.Repeat("─", halfWidth))
	cyan.Printf("┤\n")

	// ═══════════════════════════════════════════════════════════════════════
	// BOTTOM ROW: Opportunities (left) | Logs (right)
	// ═══════════════════════════════════════════════════════════════════════

	// Section headers
	cyan.Printf("│")
	cyan.Printf(" 🎯 ML SIGNALS & OPPORTUNITIES")
	cyan.Printf("%s", strings.Repeat(" ", halfWidth-31))
	cyan.Printf("│")
	cyan.Printf(" 📋 ACTIVITY LOG")
	cyan.Printf("%s", strings.Repeat(" ", halfWidth-17))
	cyan.Printf("│\n")

	// Prepare opportunity lines
	oppLines := make([]string, 0)
	oppLines = append(oppLines, fmt.Sprintf(" %-4s │ %-4s │ %-5s │ %-5s │ %-25s", "ASSET", "SIDE", "PRICE", "PROB", "SIGNAL"))
	oppLines = append(oppLines, " "+strings.Repeat("─", halfWidth-3))

	if len(d.opportunities) == 0 {
		oppLines = append(oppLines, " Scanning for opportunities...")
	} else {
		for _, opp := range d.opportunities {
			priceStr := opp.Price.Mul(decimal.NewFromInt(100)).StringFixed(0) + "¢"
			probStr := opp.Probability.Mul(decimal.NewFromInt(100)).StringFixed(0) + "%"
			reason := opp.Reason
			if len(reason) > 25 {
				reason = reason[:22] + "..."
			}
			oppLines = append(oppLines, fmt.Sprintf(" %-4s │ %-4s │ %-5s │ %-5s │ %-25s",
				opp.Asset, opp.Side, priceStr, probStr, reason))
		}
	}

	// Prepare log lines
	logLines := make([]string, 0)
	if len(d.logs) == 0 {
		logLines = append(logLines, " Waiting for activity...")
	} else {
		for _, l := range d.logs {
			if len(l) > halfWidth-2 {
				l = l[:halfWidth-5] + "..."
			}
			logLines = append(logLines, " "+l)
		}
	}

	// Print bottom section
	maxBotLines := 8
	for i := 0; i < maxBotLines; i++ {
		cyan.Printf("│")

		// Left side - opportunities
		if i < len(oppLines) {
			line := oppLines[i]
			if len(line) > halfWidth-1 {
				line = line[:halfWidth-1]
			}

			if strings.Contains(line, "🟢") || strings.Contains(line, "BUY") {
				green.Printf("%-*s", halfWidth, line)
			} else if strings.Contains(line, "🔴") || strings.Contains(line, "SKIP") {
				red.Printf("%-*s", halfWidth, line)
			} else {
				white.Printf("%-*s", halfWidth, line)
			}
		} else {
			fmt.Printf("%s", strings.Repeat(" ", halfWidth))
		}

		cyan.Printf("│")

		// Right side - logs
		if i < len(logLines) {
			line := logLines[i]
			if len(line) > halfWidth-1 {
				line = line[:halfWidth-1]
			}
			dim.Printf("%-*s", halfWidth, line)
		} else {
			fmt.Printf("%s", strings.Repeat(" ", halfWidth))
		}

		cyan.Printf("│\n")
	}

	// Bottom border
	cyan.Printf("└")
	cyan.Printf("%s", strings.Repeat("─", halfWidth))
	cyan.Printf("┴")
	cyan.Printf("%s", strings.Repeat("─", halfWidth))
	cyan.Printf("┘\n")

	// Footer with recent trades
	if len(d.recentTrades) > 0 {
		fmt.Printf("\n")
		cyan.Printf("📜 Recent: ")
		for i, t := range d.recentTrades {
			if i >= 3 {
				break
			}
			priceStr := t.Price.Mul(decimal.NewFromInt(100)).StringFixed(0) + "¢"
			if t.Action == "BUY" {
				green.Printf("%s %s %s@%s ", t.Result, t.Action, t.Asset, priceStr)
			} else if t.PnL.GreaterThan(decimal.Zero) {
				green.Printf("%s %s %s %s ", t.Result, t.Action, t.Asset, d.formatPnL(t.PnL))
			} else {
				red.Printf("%s %s %s %s ", t.Result, t.Action, t.Asset, d.formatPnL(t.PnL))
			}
		}
		fmt.Printf("\n")
	}

	// Status bar
	dim.Printf("\nBalance: $%.2f │ Press Ctrl+C to stop │ Dashboard mode active\n", d.balance.InexactFloat64())
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

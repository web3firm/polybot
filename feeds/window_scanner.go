package feeds

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

// ═══════════════════════════════════════════════════════════════════════════════
// WINDOW SCANNER - Tracks active 15-minute crypto windows
// ═══════════════════════════════════════════════════════════════════════════════
//
// Scans Polymarket for:
//   - BTC Above $X in 15 minutes?
//   - ETH Above $X in 15 minutes?
//   - SOL Above $X in 15 minutes?
//
// Tracks:
//   - Window end time (for "time remaining" calculation)
//   - Price to beat (from Polymarket question)
//   - Binance start price (snapshot when window detected)
//   - Current odds (YES/NO)
//
// Price Discovery:
//   - Polymarket uses Chainlink Data Streams (paid)
//   - We use Binance spot price (close enough, free, 100ms)
//   - Snapshot Binance price when window first detected
//   - Store in DB for historical analysis
//
// ═══════════════════════════════════════════════════════════════════════════════

const (
	polymarketAPI   = "https://gamma-api.polymarket.com"
	windowScanFreq  = 10 * time.Second
)

// SnapshotSaver interface for database
type SnapshotSaver interface {
	SaveWindowSnapshot(marketID, asset string, priceToBeat, binancePrice, yesPrice, noPrice decimal.Decimal, windowEnd time.Time) error
	UpdateWindowOutcome(marketID string, binanceEndPrice decimal.Decimal, outcome string) error
}

// Window represents an active 15-minute market window
type Window struct {
	ID            string          // Market/condition ID
	Asset         string          // "BTC", "ETH", "SOL"
	PriceToBeat   decimal.Decimal // e.g., 105000 for "BTC > $105,000"
	EndTime       time.Time       // When the window closes
	YesTokenID    string          // Token ID for YES outcome
	NoTokenID     string          // Token ID for NO outcome
	YesPrice      decimal.Decimal // Current YES odds
	NoPrice       decimal.Decimal // Current NO odds
	Question      string          // Full question text
	StartPrice    decimal.Decimal // Binance price at window detection (cached)
	LastUpdated   time.Time
}

// TimeRemaining returns duration until window closes
func (w *Window) TimeRemaining() time.Duration {
	return time.Until(w.EndTime)
}

// TimeRemainingSeconds returns seconds until window closes
func (w *Window) TimeRemainingSeconds() float64 {
	return w.TimeRemaining().Seconds()
}

// IsInSniperZone returns true if window is in last 15-60 seconds
func (w *Window) IsInSniperZone(minSec, maxSec float64) bool {
	remaining := w.TimeRemainingSeconds()
	return remaining >= minSec && remaining <= maxSec
}

// IsExpired returns true if window has ended
func (w *Window) IsExpired() bool {
	return time.Now().After(w.EndTime)
}

// PriceFeed interface for price sources
type PriceFeed interface {
	GetPrice(symbol string) decimal.Decimal
}

// WindowScanner manages window discovery and tracking
type WindowScanner struct {
	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}

	// Active windows by market ID
	windows map[string]*Window

	// Price feed (Chainlink or Binance)
	priceFeed PriceFeed

	// Database for snapshots (optional)
	db SnapshotSaver

	// Subscribers
	subscribers []chan *Window
}

// NewWindowScanner creates a new scanner
func NewWindowScanner(priceFeed PriceFeed) *WindowScanner {
	return &WindowScanner{
		stopCh:      make(chan struct{}),
		windows:     make(map[string]*Window),
		priceFeed:   priceFeed,
		subscribers: make([]chan *Window, 0),
	}
}

// SetDatabase attaches database for snapshot storage
func (s *WindowScanner) SetDatabase(db SnapshotSaver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

// Start begins scanning for windows
func (s *WindowScanner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.scanLoop()
	log.Info().Msg("🔍 Window scanner started")
}

// Stop stops the scanner
func (s *WindowScanner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	close(s.stopCh)
	log.Info().Msg("Window scanner stopped")
}

// Subscribe returns a channel that receives window updates
func (s *WindowScanner) Subscribe() chan *Window {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *Window, 100)
	s.subscribers = append(s.subscribers, ch)
	return ch
}

// GetWindow returns a window by ID
func (s *WindowScanner) GetWindow(marketID string) *Window {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.windows[marketID]
}

// GetActiveWindows returns all non-expired windows
func (s *WindowScanner) GetActiveWindows() []*Window {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Window
	for _, w := range s.windows {
		if !w.IsExpired() {
			result = append(result, w)
		}
	}
	return result
}

// GetSniperReadyWindows returns windows in sniper zone
func (s *WindowScanner) GetSniperReadyWindows(minSec, maxSec float64) []*Window {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Window
	for _, w := range s.windows {
		if w.IsInSniperZone(minSec, maxSec) {
			result = append(result, w)
		}
	}
	return result
}

// scanLoop periodically fetches active windows
func (s *WindowScanner) scanLoop() {
	ticker := time.NewTicker(windowScanFreq)
	defer ticker.Stop()

	// Initial scan
	s.fetchWindows()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.fetchWindows()
			s.cleanupExpired()
		}
	}
}

// fetchWindows gets active 15-minute crypto windows from Polymarket
func (s *WindowScanner) fetchWindows() {
	// Search for active crypto price windows
	// Look for markets with "BTC", "ETH", "SOL" and "15 minutes" or "minute" timeframe
	assets := []string{"BTC", "ETH", "SOL"}

	for _, asset := range assets {
		s.fetchAssetWindows(asset)
	}
}

// fetchAssetWindows fetches windows for a specific asset
func (s *WindowScanner) fetchAssetWindows(asset string) {
	// Query Polymarket for active markets
	url := fmt.Sprintf("%s/markets?active=true&closed=false", polymarketAPI)

	resp, err := http.Get(url)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to fetch markets")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var markets []struct {
		ID             string    `json:"id"`
		ConditionID    string    `json:"condition_id"`
		Question       string    `json:"question"`
		EndDate        time.Time `json:"end_date_iso"`
		Tokens         []struct {
			TokenID string `json:"token_id"`
			Outcome string `json:"outcome"`
		} `json:"tokens"`
		OutcomePrices string `json:"outcomePrices"` // JSON string "[0.55, 0.45]"
	}

	if err := json.Unmarshal(body, &markets); err != nil {
		log.Debug().Err(err).Msg("Failed to parse markets")
		return
	}

	// Filter for relevant windows
	for _, m := range markets {
		// Must be a 15-minute window for the asset
		if !strings.Contains(strings.ToUpper(m.Question), asset) {
			continue
		}
		if !strings.Contains(m.Question, "15 minute") && !strings.Contains(m.Question, "minute") {
			continue
		}
		if !strings.Contains(m.Question, "above") && !strings.Contains(m.Question, "Above") {
			continue
		}

		// Parse the question for price to beat
		priceToBeat := extractPriceFromQuestion(m.Question)
		if priceToBeat.IsZero() {
			continue
		}

		// Parse outcome prices
		var prices []float64
		if err := json.Unmarshal([]byte(m.OutcomePrices), &prices); err != nil || len(prices) < 2 {
			continue
		}

		// Find YES/NO token IDs
		var yesTokenID, noTokenID string
		for _, t := range m.Tokens {
			if t.Outcome == "Yes" {
				yesTokenID = t.TokenID
			} else if t.Outcome == "No" {
				noTokenID = t.TokenID
			}
		}

		// Get start price from Chainlink-aligned feed
		startPrice := s.priceFeed.GetPrice(asset)

		window := &Window{
			ID:          m.ConditionID,
			Asset:       asset,
			PriceToBeat: priceToBeat,
			EndTime:     m.EndDate,
			YesTokenID:  yesTokenID,
			NoTokenID:   noTokenID,
			YesPrice:    decimal.NewFromFloat(prices[0]),
			NoPrice:     decimal.NewFromFloat(prices[1]),
			Question:    m.Question,
			StartPrice:  startPrice,
			LastUpdated: time.Now(),
		}

		s.updateWindow(window)
	}
}

// updateWindow adds or updates a window
func (s *WindowScanner) updateWindow(window *Window) {
	s.mu.Lock()
	existing, exists := s.windows[window.ID]
	isNew := !exists
	if isNew {
		// New window - cache the start price from Binance
		s.windows[window.ID] = window
	} else {
		// Update prices only
		existing.YesPrice = window.YesPrice
		existing.NoPrice = window.NoPrice
		existing.LastUpdated = time.Now()
	}
	db := s.db
	s.mu.Unlock()

	// Save snapshot to database for new windows
	if isNew {
		log.Info().
			Str("asset", window.Asset).
			Str("target", window.PriceToBeat.StringFixed(0)).
			Str("binance", window.StartPrice.StringFixed(2)).
			Dur("remaining", window.TimeRemaining()).
			Msg("🎯 New window detected")

		// Save to DB if available
		if db != nil {
			if err := db.SaveWindowSnapshot(
				window.ID,
				window.Asset,
				window.PriceToBeat,
				window.StartPrice,
				window.YesPrice,
				window.NoPrice,
				window.EndTime,
			); err != nil {
				log.Warn().Err(err).Msg("Failed to save window snapshot")
			}
		}
	}

	// Broadcast to subscribers
	s.broadcast(window)
}

// broadcast sends window to all subscribers
func (s *WindowScanner) broadcast(window *Window) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.subscribers {
		select {
		case ch <- window:
		default:
		}
	}
}

// cleanupExpired removes expired windows and records outcomes
func (s *WindowScanner) cleanupExpired() {
	s.mu.Lock()
	var expired []*Window
	for id, w := range s.windows {
		if w.IsExpired() {
			expired = append(expired, w)
			delete(s.windows, id)
		}
	}
	db := s.db
	pf := s.priceFeed
	s.mu.Unlock()

	// Record outcomes for expired windows
	for _, w := range expired {
		// Get final price from Chainlink feed
		endPrice := pf.GetPrice(w.Asset)

		// Determine outcome
		outcome := "NO"
		if endPrice.GreaterThanOrEqual(w.PriceToBeat) {
			outcome = "YES"
		}

		log.Debug().
			Str("asset", w.Asset).
			Str("outcome", outcome).
			Str("end_price", endPrice.StringFixed(2)).
			Str("target", w.PriceToBeat.StringFixed(0)).
			Msg("Window expired")

		// Update database
		if db != nil {
			db.UpdateWindowOutcome(w.ID, endPrice, outcome)
		}
	}
}

// extractPriceFromQuestion parses "BTC above $105,000" -> 105000
func extractPriceFromQuestion(question string) decimal.Decimal {
	// Look for $ followed by numbers
	// Examples:
	//   "BTC above $105,000 in 15 minutes"
	//   "ETH above $3,500 in 15 minutes"

	parts := strings.Split(question, "$")
	if len(parts) < 2 {
		return decimal.Zero
	}

	// Get the price part after $
	pricePart := parts[1]

	// Extract digits and commas
	var priceStr strings.Builder
	for _, c := range pricePart {
		if c >= '0' && c <= '9' {
			priceStr.WriteRune(c)
		} else if c == ',' {
			continue // Skip commas
		} else if c == '.' {
			priceStr.WriteRune(c)
		} else {
			break // Stop at first non-digit
		}
	}

	price, err := decimal.NewFromString(priceStr.String())
	if err != nil {
		return decimal.Zero
	}
	return price
}

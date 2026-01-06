// Package polymarket provides Polymarket API integration
//
// window_scanner.go - Scans for crypto prediction windows on Polymarket
// Searches for "Will [ASSET] go up/down in the next X minutes?" markets
// Asset is configurable via constructor (BTC, ETH, SOL, etc.)
package polymarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

// PredictionWindow represents a crypto up/down prediction window market
type PredictionWindow struct {
	ID            string
	ConditionID   string
	Question      string
	Slug          string
	Asset         string // BTC, ETH, SOL, etc.

	// Tokens
	YesTokenID string
	NoTokenID  string

	// Prices (YES = "Up", NO = "Down")
	YesPrice decimal.Decimal // Price for "Up" outcome
	NoPrice  decimal.Decimal // Price for "Down" outcome

	// Market info
	Volume    decimal.Decimal
	Liquidity decimal.Decimal
	EndDate   time.Time
	Active    bool
	Closed    bool

	// Parsed info
	WindowMinutes int    // 15, 60, etc.
	WindowType    string // "up_down", "price_range", etc.

	LastUpdated time.Time
}

// WindowScanner scans for crypto prediction window markets
type WindowScanner struct {
	client  *Client
	restURL string
	asset   string // The asset to scan for (BTC, ETH, etc.)

	windows   []PredictionWindow
	windowsMu sync.RWMutex

	onNewWindow func(PredictionWindow)

	running bool
	stopCh  chan struct{}
}

// NewWindowScanner creates a new scanner for the given asset
func NewWindowScanner(apiURL string, asset string) *WindowScanner {
	return &WindowScanner{
		client:  NewClient(apiURL),
		restURL: apiURL,
		asset:   strings.ToUpper(asset),
		windows: make([]PredictionWindow, 0),
		stopCh:  make(chan struct{}),
	}
}

// SetNewWindowCallback sets callback for new windows
func (s *WindowScanner) SetNewWindowCallback(cb func(PredictionWindow)) {
	s.onNewWindow = cb
}

// Start begins scanning for prediction windows
func (s *WindowScanner) Start() {
	s.running = true
	go s.scanLoop()
	log.Info().Str("asset", s.asset).Msg("🔍 Window Scanner started")
}

// Stop stops the scanner
func (s *WindowScanner) Stop() {
	s.running = false
	close(s.stopCh)
}

func (s *WindowScanner) scanLoop() {
	// Scan immediately
	s.scan()

	// Then scan every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.scan()
		case <-s.stopCh:
			return
		}
	}
}

func (s *WindowScanner) scan() {
	windows, err := s.fetchWindows()
	if err != nil {
		log.Error().Err(err).Str("asset", s.asset).Msg("Failed to fetch windows")
		return
	}

	s.windowsMu.Lock()
	oldWindows := make(map[string]bool)
	for _, w := range s.windows {
		oldWindows[w.ID] = true
	}

	s.windows = windows
	s.windowsMu.Unlock()

	// Notify about new windows
	for _, w := range windows {
		if !oldWindows[w.ID] && s.onNewWindow != nil {
			s.onNewWindow(w)
		}
	}

	log.Debug().Int("windows", len(windows)).Str("asset", s.asset).Msg("Windows updated")
}

func (s *WindowScanner) fetchWindows() ([]PredictionWindow, error) {
	// Build search terms based on asset
	var searchTerms []string

	switch s.asset {
	case "BTC":
		searchTerms = []string{"Bitcoin", "BTC", "bitcoin up", "bitcoin down"}
	case "ETH":
		searchTerms = []string{"Ethereum", "ETH", "ethereum up", "ethereum down"}
	case "SOL":
		searchTerms = []string{"Solana", "SOL", "solana up", "solana down"}
	default:
		searchTerms = []string{s.asset, strings.ToLower(s.asset) + " up", strings.ToLower(s.asset) + " down"}
	}

	allWindows := make([]PredictionWindow, 0)
	seen := make(map[string]bool)

	for _, term := range searchTerms {
		windows, err := s.searchMarkets(term)
		if err != nil {
			continue
		}

		for _, w := range windows {
			if !seen[w.ID] {
				seen[w.ID] = true
				allWindows = append(allWindows, w)
			}
		}
	}

	return allWindows, nil
}

func (s *WindowScanner) searchMarkets(query string) ([]PredictionWindow, error) {
	// Build search URL
	params := url.Values{}
	params.Set("closed", "false")
	params.Set("active", "true")
	params.Set("limit", "50")

	searchURL := fmt.Sprintf("%s/markets?%s", s.restURL, params.Encode())

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var markets []Market
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, err
	}

	windows := make([]PredictionWindow, 0)

	for _, m := range markets {
		questionLower := strings.ToLower(m.Question)

		// Filter for asset-related markets
		if !s.matchesAsset(questionLower) {
			continue
		}

		// Look for up/down prediction markets
		isUpDown := strings.Contains(questionLower, "up") ||
			strings.Contains(questionLower, "down") ||
			strings.Contains(questionLower, "higher") ||
			strings.Contains(questionLower, "lower") ||
			strings.Contains(questionLower, "above") ||
			strings.Contains(questionLower, "below")

		if !isUpDown {
			continue
		}

		window := s.parseMarketToWindow(m)
		if window != nil {
			windows = append(windows, *window)
		}
	}

	return windows, nil
}

func (s *WindowScanner) matchesAsset(questionLower string) bool {
	switch s.asset {
	case "BTC":
		return strings.Contains(questionLower, "bitcoin") || strings.Contains(questionLower, "btc")
	case "ETH":
		return strings.Contains(questionLower, "ethereum") || strings.Contains(questionLower, "eth")
	case "SOL":
		return strings.Contains(questionLower, "solana") || strings.Contains(questionLower, "sol")
	default:
		return strings.Contains(questionLower, strings.ToLower(s.asset))
	}
}

func (s *WindowScanner) parseMarketToWindow(m Market) *PredictionWindow {
	// Parse outcomes and prices
	var outcomes []string
	if err := json.Unmarshal([]byte(m.Outcomes), &outcomes); err != nil {
		return nil
	}

	var prices []string
	if err := json.Unmarshal([]byte(m.OutcomePrices), &prices); err != nil {
		return nil
	}

	if len(outcomes) < 2 || len(prices) < 2 {
		return nil
	}

	yesPrice, _ := decimal.NewFromString(prices[0])
	noPrice, _ := decimal.NewFromString(prices[1])
	volume, _ := decimal.NewFromString(m.Volume)

	// Parse end date
	var endDate time.Time
	if m.EndDate != "" {
		endDate, _ = time.Parse(time.RFC3339, m.EndDate)
	}

	// Detect window type
	windowMinutes := 15 // Default
	questionLower := strings.ToLower(m.Question)
	if strings.Contains(questionLower, "1 hour") || strings.Contains(questionLower, "60 min") {
		windowMinutes = 60
	} else if strings.Contains(questionLower, "5 min") {
		windowMinutes = 5
	} else if strings.Contains(questionLower, "30 min") {
		windowMinutes = 30
	}

	return &PredictionWindow{
		ID:            m.ID,
		ConditionID:   m.ConditionID,
		Question:      m.Question,
		Slug:          m.Slug,
		Asset:         s.asset,
		YesPrice:      yesPrice,
		NoPrice:       noPrice,
		Volume:        volume,
		EndDate:       endDate,
		Active:        m.Active,
		Closed:        m.Closed,
		WindowMinutes: windowMinutes,
		WindowType:    "up_down",
		LastUpdated:   time.Now(),
	}
}

// GetActiveWindows returns currently active prediction windows
func (s *WindowScanner) GetActiveWindows() []PredictionWindow {
	s.windowsMu.RLock()
	defer s.windowsMu.RUnlock()

	active := make([]PredictionWindow, 0)
	now := time.Now()

	for _, w := range s.windows {
		// Only include windows that haven't ended
		if w.Active && !w.Closed && (w.EndDate.IsZero() || w.EndDate.After(now)) {
			active = append(active, w)
		}
	}

	return active
}

// GetWindowByID returns a specific window
func (s *WindowScanner) GetWindowByID(id string) *PredictionWindow {
	s.windowsMu.RLock()
	defer s.windowsMu.RUnlock()

	for _, w := range s.windows {
		if w.ID == id {
			return &w
		}
	}
	return nil
}

// GetBestWindow returns the window with best liquidity/odds
func (s *WindowScanner) GetBestWindow() *PredictionWindow {
	windows := s.GetActiveWindows()
	if len(windows) == 0 {
		return nil
	}

	// Find window with highest volume (most liquid)
	best := &windows[0]
	for i := range windows {
		if windows[i].Volume.GreaterThan(best.Volume) {
			best = &windows[i]
		}
	}

	return best
}

package scanner

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"

	"github.com/web3guy0/polybot/internal/config"
	"github.com/web3guy0/polybot/internal/database"
	"github.com/web3guy0/polybot/internal/polymarket"
	"github.com/web3guy0/polybot/internal/trading"
)

type OpportunityType string

const (
	TypeUnderpriced    OpportunityType = "underpriced"
	TypeOverpriced     OpportunityType = "overpriced"
	TypeSevereMispricing OpportunityType = "severe_mispricing"
)

type Opportunity struct {
	Market     polymarket.ParsedMarket
	YesPrice   decimal.Decimal
	NoPrice    decimal.Decimal
	TotalPrice decimal.Decimal
	Spread     decimal.Decimal
	SpreadPct  decimal.Decimal
	Type       OpportunityType
	Timestamp  time.Time
}

type Scanner struct {
	cfg           *config.Config
	db            *database.Database
	client        *polymarket.Client
	tradingEngine *trading.Engine
	
	opportunities []Opportunity
	mu            sync.RWMutex
	
	subscribers   []chan Opportunity
	subMu         sync.RWMutex
	
	stopCh        chan struct{}
	running       bool
}

func New(cfg *config.Config, db *database.Database, engine *trading.Engine) *Scanner {
	return &Scanner{
		cfg:           cfg,
		db:            db,
		client:        polymarket.NewClient(cfg.PolymarketAPIURL),
		tradingEngine: engine,
		opportunities: make([]Opportunity, 0),
		subscribers:   make([]chan Opportunity, 0),
		stopCh:        make(chan struct{}),
	}
}

func (s *Scanner) Start() {
	s.running = true
	log.Info().
		Dur("interval", s.cfg.ScanInterval).
		Str("min_spread", s.cfg.MinSpreadPct.String()).
		Msg("📡 Market scanner started")

	ticker := time.NewTicker(s.cfg.ScanInterval)
	defer ticker.Stop()

	// Run immediately
	s.scan()

	for {
		select {
		case <-ticker.C:
			s.scan()
		case <-s.stopCh:
			log.Info().Msg("Scanner stopped")
			return
		}
	}
}

func (s *Scanner) Stop() {
	if s.running {
		close(s.stopCh)
		s.running = false
	}
}

func (s *Scanner) Subscribe() chan Opportunity {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	
	ch := make(chan Opportunity, 100)
	s.subscribers = append(s.subscribers, ch)
	return ch
}

func (s *Scanner) GetOpportunities() []Opportunity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	result := make([]Opportunity, len(s.opportunities))
	copy(result, s.opportunities)
	return result
}

func (s *Scanner) scan() {
	markets, err := s.client.GetMarkets(s.cfg.MaxMarketsToScan)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch markets")
		return
	}

	opportunities := s.findOpportunities(markets)
	
	s.mu.Lock()
	s.opportunities = opportunities
	s.mu.Unlock()

	log.Info().
		Int("markets_scanned", len(markets)).
		Int("opportunities_found", len(opportunities)).
		Msg("Scan complete")

	// Notify subscribers
	s.notifySubscribers(opportunities)

	// Auto-trade if enabled
	if s.cfg.TradingEnabled && !s.cfg.DryRun {
		for _, opp := range opportunities {
			if opp.SpreadPct.GreaterThanOrEqual(s.cfg.MinProfitPct) {
				s.tradingEngine.ExecuteArbitrage(opp.Market, opp)
			}
		}
	}
}

func (s *Scanner) findOpportunities(markets []polymarket.ParsedMarket) []Opportunity {
	var opportunities []Opportunity
	one := decimal.NewFromFloat(1.0)

	for _, market := range markets {
		if market.Closed || market.YesPrice.IsZero() || market.NoPrice.IsZero() {
			continue
		}

		totalPrice := market.YesPrice.Add(market.NoPrice)
		spread := totalPrice.Sub(one).Abs()
		spreadPct := spread.Mul(decimal.NewFromFloat(100))

		// Skip if spread below threshold
		if spreadPct.LessThan(s.cfg.MinSpreadPct) {
			continue
		}

		var oppType OpportunityType
		
		// Determine opportunity type
		if totalPrice.LessThan(one) {
			oppType = TypeUnderpriced // Buy both for guaranteed profit
		} else if totalPrice.GreaterThan(decimal.NewFromFloat(1.02)) {
			oppType = TypeOverpriced
		}
		
		if totalPrice.GreaterThan(decimal.NewFromFloat(1.10)) {
			oppType = TypeSevereMispricing
		}

		if oppType == "" {
			continue
		}

		opp := Opportunity{
			Market:     market,
			YesPrice:   market.YesPrice,
			NoPrice:    market.NoPrice,
			TotalPrice: totalPrice,
			Spread:     spread,
			SpreadPct:  spreadPct,
			Type:       oppType,
			Timestamp:  time.Now(),
		}

		opportunities = append(opportunities, opp)

		// Save to database
		dbOpp := &database.Opportunity{
			MarketID:   market.ID,
			Question:   market.Question,
			YesPrice:   market.YesPrice,
			NoPrice:    market.NoPrice,
			TotalPrice: totalPrice,
			SpreadPct:  spreadPct,
			Type:       string(oppType),
		}
		s.db.SaveOpportunity(dbOpp)
	}

	return opportunities
}

func (s *Scanner) notifySubscribers(opportunities []Opportunity) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()

	for _, opp := range opportunities {
		for _, ch := range s.subscribers {
			select {
			case ch <- opp:
			default:
				// Channel full, skip
			}
		}
	}
}

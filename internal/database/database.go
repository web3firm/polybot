package database

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Database struct {
	db *gorm.DB
}

// Models

type Market struct {
	ID            string `gorm:"primaryKey"`
	Question      string
	Slug          string
	YesPrice      decimal.Decimal `gorm:"type:decimal(10,6)"`
	NoPrice       decimal.Decimal `gorm:"type:decimal(10,6)"`
	Volume        decimal.Decimal `gorm:"type:decimal(20,2)"`
	EndDate       time.Time
	Active        bool
	LastChecked   time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Opportunity struct {
	ID          uint            `gorm:"primaryKey;autoIncrement"`
	MarketID    string          `gorm:"index"`
	Question    string
	YesPrice    decimal.Decimal `gorm:"type:decimal(10,6)"`
	NoPrice     decimal.Decimal `gorm:"type:decimal(10,6)"`
	TotalPrice  decimal.Decimal `gorm:"type:decimal(10,6)"`
	SpreadPct   decimal.Decimal `gorm:"type:decimal(10,4)"`
	Type        string
	AlertSent   bool
	TradedAt    *time.Time
	Profit      decimal.Decimal `gorm:"type:decimal(20,6)"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Trade struct {
	ID           uint            `gorm:"primaryKey;autoIncrement"`
	MarketID     string          `gorm:"index"`
	Side         string          // "YES" or "NO"
	Amount       decimal.Decimal `gorm:"type:decimal(20,6)"`
	Price        decimal.Decimal `gorm:"type:decimal(10,6)"`
	Status       string          // "pending", "executed", "failed"
	TxHash       string
	ProfitLoss   decimal.Decimal `gorm:"type:decimal(20,6)"`
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Alert struct {
	ID         uint   `gorm:"primaryKey;autoIncrement"`
	MarketID   string `gorm:"index"`
	ChatID     int64
	MessageID  int
	Type       string
	SpreadPct  decimal.Decimal `gorm:"type:decimal(10,4)"`
	CreatedAt  time.Time
}

type UserSettings struct {
	ChatID          int64 `gorm:"primaryKey"`
	AlertsEnabled   bool  `gorm:"default:true"`
	MinSpreadPct    decimal.Decimal `gorm:"type:decimal(10,4);default:2.0"`
	TradingEnabled  bool  `gorm:"default:false"`
	MaxTradeSize    decimal.Decimal `gorm:"type:decimal(20,6);default:100"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func New(dbPath string) (*Database, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	// Auto migrate
	if err := db.AutoMigrate(&Market{}, &Opportunity{}, &Trade{}, &Alert{}, &UserSettings{}); err != nil {
		return nil, err
	}

	log.Info().Str("path", dbPath).Msg("Database initialized")

	return &Database{db: db}, nil
}

// Market operations

func (d *Database) SaveMarket(market *Market) error {
	return d.db.Save(market).Error
}

func (d *Database) GetMarket(id string) (*Market, error) {
	var market Market
	err := d.db.First(&market, "id = ?", id).Error
	return &market, err
}

// Opportunity operations

func (d *Database) SaveOpportunity(opp *Opportunity) error {
	return d.db.Create(opp).Error
}

func (d *Database) GetRecentOpportunities(limit int) ([]Opportunity, error) {
	var opps []Opportunity
	err := d.db.Order("created_at DESC").Limit(limit).Find(&opps).Error
	return opps, err
}

func (d *Database) GetLastAlertTime(marketID string) (time.Time, error) {
	var alert Alert
	err := d.db.Where("market_id = ?", marketID).Order("created_at DESC").First(&alert).Error
	if err != nil {
		return time.Time{}, err
	}
	return alert.CreatedAt, nil
}

// Alert operations

func (d *Database) SaveAlert(alert *Alert) error {
	return d.db.Create(alert).Error
}

// Trade operations

func (d *Database) SaveTrade(trade *Trade) error {
	return d.db.Create(trade).Error
}

func (d *Database) UpdateTrade(trade *Trade) error {
	return d.db.Save(trade).Error
}

func (d *Database) GetTradesByMarket(marketID string) ([]Trade, error) {
	var trades []Trade
	err := d.db.Where("market_id = ?", marketID).Order("created_at DESC").Find(&trades).Error
	return trades, err
}

func (d *Database) GetTotalProfitLoss() (decimal.Decimal, error) {
	var result struct {
		Total decimal.Decimal
	}
	err := d.db.Model(&Trade{}).Select("COALESCE(SUM(profit_loss), 0) as total").Scan(&result).Error
	return result.Total, err
}

// User settings operations

func (d *Database) GetUserSettings(chatID int64) (*UserSettings, error) {
	var settings UserSettings
	err := d.db.FirstOrCreate(&settings, UserSettings{ChatID: chatID}).Error
	return &settings, err
}

func (d *Database) SaveUserSettings(settings *UserSettings) error {
	return d.db.Save(settings).Error
}

// Stats operations

func (d *Database) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var opportunityCount int64
	d.db.Model(&Opportunity{}).Count(&opportunityCount)
	stats["total_opportunities"] = opportunityCount

	var tradeCount int64
	d.db.Model(&Trade{}).Count(&tradeCount)
	stats["total_trades"] = tradeCount

	pnl, _ := d.GetTotalProfitLoss()
	stats["total_pnl"] = pnl

	var marketCount int64
	d.db.Model(&Market{}).Where("active = ?", true).Count(&marketCount)
	stats["active_markets"] = marketCount

	return stats, nil
}

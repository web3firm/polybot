package polymarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

// CLOBPriceFetcher fetches real-time prices directly from CLOB orderbook
type CLOBPriceFetcher struct {
	client *http.Client
}

// OrderbookResponse from CLOB API
type OrderbookResponse struct {
	Market string `json:"market"`
	Asset  string `json:"asset_id"`
	Bids   []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"bids"`
	Asks []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"asks"`
	Timestamp int64 `json:"timestamp"`
}

// MidpointResponse from CLOB API
type MidpointResponse struct {
	Mid string `json:"mid"`
}

// PriceResponse from CLOB API
type PriceResponse struct {
	Price string `json:"price"`
}

// NewCLOBPriceFetcher creates a new CLOB price fetcher
func NewCLOBPriceFetcher() *CLOBPriceFetcher {
	return &CLOBPriceFetcher{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// GetMidpoint fetches the midpoint price for a token
func (c *CLOBPriceFetcher) GetMidpoint(tokenID string) (decimal.Decimal, error) {
	url := fmt.Sprintf("https://clob.polymarket.com/midpoint?token_id=%s", tokenID)
	
	resp, err := c.client.Get(url)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return decimal.Zero, fmt.Errorf("CLOB API returned %d", resp.StatusCode)
	}
	
	var result MidpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return decimal.Zero, err
	}
	
	return decimal.NewFromString(result.Mid)
}

// GetPrice fetches the last trade price for a token
func (c *CLOBPriceFetcher) GetPrice(tokenID string) (decimal.Decimal, error) {
	url := fmt.Sprintf("https://clob.polymarket.com/price?token_id=%s&side=buy", tokenID)
	
	resp, err := c.client.Get(url)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return decimal.Zero, fmt.Errorf("CLOB API returned %d", resp.StatusCode)
	}
	
	var result PriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return decimal.Zero, err
	}
	
	return decimal.NewFromString(result.Price)
}

// GetBestBidAsk fetches orderbook and returns best bid/ask
func (c *CLOBPriceFetcher) GetBestBidAsk(tokenID string) (bid, ask decimal.Decimal, err error) {
	url := fmt.Sprintf("https://clob.polymarket.com/book?token_id=%s", tokenID)
	
	resp, err := c.client.Get(url)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return decimal.Zero, decimal.Zero, fmt.Errorf("CLOB API returned %d", resp.StatusCode)
	}
	
	var result OrderbookResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	
	// Best bid is highest bid
	if len(result.Bids) > 0 {
		bid, _ = decimal.NewFromString(result.Bids[0].Price)
	}
	
	// Best ask is lowest ask
	if len(result.Asks) > 0 {
		ask, _ = decimal.NewFromString(result.Asks[0].Price)
	}
	
	return bid, ask, nil
}

// GetLivePrices fetches live prices for UP and DOWN tokens
func (c *CLOBPriceFetcher) GetLivePrices(upTokenID, downTokenID string) (upPrice, downPrice decimal.Decimal, err error) {
	// Fetch both in parallel for speed
	type result struct {
		price decimal.Decimal
		err   error
	}
	
	upCh := make(chan result, 1)
	downCh := make(chan result, 1)
	
	go func() {
		price, err := c.GetMidpoint(upTokenID)
		upCh <- result{price, err}
	}()
	
	go func() {
		price, err := c.GetMidpoint(downTokenID)
		downCh <- result{price, err}
	}()
	
	upResult := <-upCh
	downResult := <-downCh
	
	if upResult.err != nil {
		log.Debug().Err(upResult.err).Str("token", upTokenID).Msg("Failed to get UP price")
	}
	if downResult.err != nil {
		log.Debug().Err(downResult.err).Str("token", downTokenID).Msg("Failed to get DOWN price")
	}
	
	return upResult.price, downResult.price, nil
}

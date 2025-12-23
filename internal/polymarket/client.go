package polymarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Market struct {
	ID                 string `json:"id"`
	Question           string `json:"question"`
	Slug               string `json:"slug"`
	Active             bool   `json:"active"`
	Closed             bool   `json:"closed"`
	Volume             string `json:"volume"`
	OutcomePrices      string `json:"outcomePrices"`
	Outcomes           string `json:"outcomes"`
	ConditionID        string `json:"conditionId"`
	EndDate            string `json:"endDateIso"`
	Description        string `json:"description"`
	MarketMakerAddress string `json:"marketMakerAddress"`
}

type ParsedMarket struct {
	ID       string
	Question string
	Slug     string
	Active   bool
	Closed   bool
	Volume   decimal.Decimal
	YesPrice decimal.Decimal
	NoPrice  decimal.Decimal
	Outcomes []string
	EndDate  time.Time
}

type OrderBook struct {
	Bids [][]string `json:"bids"` // [[price, size], ...]
	Asks [][]string `json:"asks"`
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetMarkets fetches a single page of markets (for backward compatibility)
func (c *Client) GetMarkets(limit int) ([]ParsedMarket, error) {
	return c.GetMarketsWithOffset(limit, 0)
}

// GetMarketsWithOffset fetches a single page of markets with offset
func (c *Client) GetMarketsWithOffset(limit, offset int) ([]ParsedMarket, error) {
	url := fmt.Sprintf("%s/markets?closed=false&active=true&limit=%d&offset=%d", c.baseURL, limit, offset)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var markets []Market
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, fmt.Errorf("failed to decode markets: %w", err)
	}

	// Parse markets
	var parsed []ParsedMarket
	for _, m := range markets {
		pm, err := c.parseMarket(m)
		if err != nil {
			log.Debug().Err(err).Str("market", m.ID).Msg("Failed to parse market")
			continue
		}
		parsed = append(parsed, *pm)
	}

	return parsed, nil
}

// GetAllMarkets fetches all markets using pagination, with optional rate limiting
func (c *Client) GetAllMarkets(batchSize, maxMarkets, maxRPS int) ([]ParsedMarket, error) {
	var all []ParsedMarket
	offset := 0
	for {
		// Respect maxMarkets if set
		toFetch := batchSize
		if maxMarkets > 0 && len(all)+batchSize > maxMarkets {
			toFetch = maxMarkets - len(all)
		}
		if toFetch <= 0 {
			break
		}

		batch, err := c.GetMarketsWithOffset(toFetch, offset)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		offset += len(batch)

		// If we got less than requested, we're done
		if len(batch) < toFetch {
			break
		}

		// Rate limiting
		if maxRPS > 0 {
			time.Sleep(time.Second / time.Duration(maxRPS))
		}
	}
	return all, nil
}

func (c *Client) GetMarket(conditionID string) (*ParsedMarket, error) {
	url := fmt.Sprintf("%s/markets/%s", c.baseURL, conditionID)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var market Market
	if err := json.NewDecoder(resp.Body).Decode(&market); err != nil {
		return nil, err
	}

	return c.parseMarket(market)
}

func (c *Client) parseMarket(m Market) (*ParsedMarket, error) {
	pm := &ParsedMarket{
		ID:       m.ID,
		Question: m.Question,
		Slug:     m.Slug,
		Active:   m.Active,
		Closed:   m.Closed,
	}

	// Parse volume
	if m.Volume != "" {
		vol, err := decimal.NewFromString(m.Volume)
		if err == nil {
			pm.Volume = vol
		}
	}

	// Parse outcome prices
	if m.OutcomePrices != "" {
		var prices []string
		if err := json.Unmarshal([]byte(m.OutcomePrices), &prices); err != nil {
			return nil, fmt.Errorf("failed to parse outcome prices: %w", err)
		}

		if len(prices) >= 2 {
			if yes, err := decimal.NewFromString(prices[0]); err == nil {
				pm.YesPrice = yes
			}
			if no, err := decimal.NewFromString(prices[1]); err == nil {
				pm.NoPrice = no
			}
		}
	}

	// Parse outcomes
	if m.Outcomes != "" {
		var outcomes []string
		if err := json.Unmarshal([]byte(m.Outcomes), &outcomes); err == nil {
			pm.Outcomes = outcomes
		}
	}

	// Parse end date
	if m.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, m.EndDate); err == nil {
			pm.EndDate = t
		}
	}

	return pm, nil
}

// GetOrderBook fetches the order book for a market (for future trading)
func (c *Client) GetOrderBook(tokenID string) (*OrderBook, error) {
	url := fmt.Sprintf("https://clob.polymarket.com/book?token_id=%s", tokenID)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var book OrderBook
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		return nil, err
	}

	return &book, nil
}

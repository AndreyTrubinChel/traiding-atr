package bcs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MarketDataService предоставляет методы для получения рыночных данных.
type MarketDataService struct {
	client *Client
}

// Candle представляет одну свечу (OHLCV).
type Candle struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume"`
}

// CandlesResponse представляет ответ API с массивом свечей.
type CandlesResponse struct {
	Ticker    string   `json:"ticker"`
	ClassCode string   `json:"classCode"`
	StartDate time.Time `json:"startDate"`
	EndDate   time.Time `json:"endDate"`
	TimeFrame string   `json:"timeFrame"`
	Bars      []Candle `json:"bars"`
}

// GetCandles получает исторические свечи для указанного инструмента.
func (s *MarketDataService) GetCandles(ticker, classCode, timeFrame string, from, to time.Time) (*CandlesResponse, error) {
	url := fmt.Sprintf("%s/candles-chart?classCode=%s&ticker=%s&startDate=%s&endDate=%s&timeFrame=%s",
		s.client.baseURL,
		classCode,
		ticker,
		from.Format("2006-01-02T15:04:05Z"),
		to.Format("2006-01-02T15:04:05Z"),
		timeFrame,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.client.accessToken)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var candlesResp CandlesResponse
	if err := json.Unmarshal(body, &candlesResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга свечей: %w", err)
	}

	return &candlesResp, nil
}
package bcs

import (
	"net/http"
	"time"
)

// Client — главный клиент для работы с API БКС.
// Объединяет все сервисы для доступа к разным разделам API.
type Client struct {
	httpClient  *http.Client
	accessToken string
	baseURL     string

	// Сервисы (группы методов)
	MarketData  *MarketDataService
	Instruments *InstrumentsService
}

// NewClient создаёт новый экземпляр клиента API БКС.
func NewClient(accessToken string) *Client {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	c := &Client{
		httpClient:  httpClient,
		accessToken: accessToken,
		baseURL:     "https://be.broker.ru/trade-api-market-data-connector/api/v1",
	}

	// Инициализируем сервисы
	c.MarketData = &MarketDataService{client: c}
	c.Instruments = &InstrumentsService{client: c}

	return c
}
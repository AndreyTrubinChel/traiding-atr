package moex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Spec содержит все данные о фьючерсе с Мосбиржи
type Spec struct {
	GoBuy        float64
	GoSell       float64
	LotSize      float64
	LastPrice    float64
	Spread       float64
	VolumeDay    float64
	OpenPrice    float64
	HighPrice    float64
	LowPrice     float64
	PrevClose    float64
	NumTrades    int
	MaturityDate string
}

// GetSpec получает все данные о фьючерсе с API Мосбиржи
func GetSpec(ticker string) (*Spec, error) {
	url := fmt.Sprintf("https://iss.moex.com/iss/engines/futures/markets/forts/securities/%s.json", ticker)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Securities struct {
			Data    [][]interface{} `json:"data"`
			Columns []string        `json:"columns"`
		} `json:"securities"`
		Marketdata struct {
			Data    [][]interface{} `json:"data"`
			Columns []string        `json:"columns"`
		} `json:"marketdata"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	spec := &Spec{}

	// Из securities
	if len(result.Securities.Data) > 0 {
		row := result.Securities.Data[0]
		for i, col := range result.Securities.Columns {
			switch col {
			case "INITIALMARGIN":
				if v, ok := row[i].(float64); ok {
					spec.GoBuy, spec.GoSell = v, v
				}
			case "LOTVOLUME":
				if v, ok := row[i].(float64); ok {
					spec.LotSize = v
				}
			case "LASTTRADEDATE":
				if v, ok := row[i].(string); ok {
					spec.MaturityDate = v
				}
			}
		}
	}

	// Из marketdata
	if len(result.Marketdata.Data) > 0 {
		row := result.Marketdata.Data[0]
		for i, col := range result.Marketdata.Columns {
			switch col {
			case "LAST":
				if v, ok := row[i].(float64); ok {
					spec.LastPrice = v
				}
			case "SPREAD":
				if v, ok := row[i].(float64); ok {
					spec.Spread = v
				}
			case "VALTODAY":
				if v, ok := row[i].(float64); ok {
					spec.VolumeDay = v
				}
			case "NUMTRADES":
				if v, ok := row[i].(float64); ok {
					spec.NumTrades = int(v)
				}
			case "OPEN":
				if v, ok := row[i].(float64); ok {
					spec.OpenPrice = v
				}
			case "HIGH":
				if v, ok := row[i].(float64); ok {
					spec.HighPrice = v
				}
			case "LOW":
				if v, ok := row[i].(float64); ok {
					spec.LowPrice = v
				}
			case "SETTLEPRICE":
				if v, ok := row[i].(float64); ok {
					spec.PrevClose = v
				}
			}
		}
	}

	return spec, nil
}
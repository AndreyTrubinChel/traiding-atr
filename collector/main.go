package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"trading-atr/collector/moex"

	_ "github.com/lib/pq"
)

const (
	BSC_TOKEN_URL   = "https://be.broker.ru/trade-api-keycloak/realms/tradeapi/protocol/openid-connect/token"
	BSC_CANDLES_URL = "https://be.broker.ru/trade-api-market-data-connector/api/v1/candles-chart"
	ATR_PERIOD      = 10
	CLASS_CODE      = "SPBFUT"
	TIMEFRAME       = "H4"
	TOKEN_FILE      = "refresh_token.txt"
)

const BSC_SPEC_URL = "https://be.broker.ru/trade-api-information-service/api/v1/instruments/by-tickers"

var NEON_DATABASE_URL = os.Getenv("NEON_DATABASE_URL")

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type Bar struct {
	Time   string  `json:"time"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
}

type CandlesResponse struct {
	Bars []Bar `json:"bars"`
}

type SpecInfo struct {
	Step      float64
	StepPrice float64
	LotSize   float64
}

func main() {
	log.Println("=== Сборщик ATR (БКС REST API) ===")

	if NEON_DATABASE_URL == "" {
		log.Fatal("❌ NEON_DATABASE_URL не задан")
	}

	refreshToken := os.Getenv("BSC_REFRESH_TOKEN")
	if refreshToken == "" {
		data, err := os.ReadFile(TOKEN_FILE)
		if err == nil {
			refreshToken = strings.TrimSpace(string(data))
		}
	}
	if refreshToken == "" {
		log.Fatal("❌ BSC_REFRESH_TOKEN не задан и файл не найден")
	}

	db, err := sql.Open("postgres", NEON_DATABASE_URL)
	if err != nil {
		log.Fatal("❌ Ошибка подключения к БД:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("❌ БД не отвечает:", err)
	}
	log.Println("✅ Подключено к Neon")

	tickers, err := getActiveTickers(db)
	if err != nil {
		log.Fatal("❌ Ошибка получения списка тикеров:", err)
	}
	log.Printf("📋 Загружено %d активных инструментов", len(tickers))

	accessToken, newRefreshToken, err := getAccessToken(refreshToken)
	if err != nil {
		log.Fatal("❌ Ошибка получения access token:", err)
	}
	log.Println("✅ Access token получен")

	// Спецификации из API БКС
	tickersList := make([]string, 0, len(tickers))
	for t := range tickers {
		tickersList = append(tickersList, t)
	}
	specs, err := getSpecs(accessToken, tickersList)
	if err != nil {
		log.Printf("⚠️ Ошибка получения спецификаций: %v", err)
	} else {
		for ticker, spec := range specs {
			_, err := db.Exec(
				`INSERT INTO instrument_specs (ticker, step, step_price, lot_size)
				 VALUES ($1, $2, $3, $4)
				 ON CONFLICT (ticker) DO UPDATE SET step=$2, step_price=$3, lot_size=$4, updated_at=NOW()`,
				ticker, spec.Step, spec.StepPrice, spec.LotSize,
			)
			if err != nil {
				log.Printf("⚠️ Ошибка сохранения спецификации %s: %v", ticker, err)
			} else {
				log.Printf("📋 Спецификация %s сохранена", ticker)
			}
		}
	}

	// Сохраняем новый refresh-токен
	if newRefreshToken != "" && newRefreshToken != refreshToken {
		if err := os.WriteFile(TOKEN_FILE, []byte(newRefreshToken), 0600); err != nil {
			log.Printf("⚠️ Не удалось сохранить refresh-токен: %v", err)
		} else {
			log.Println("✅ Refresh-токен обновлён и сохранён")
		}
	}

	// ATR
	successCount := 0
	for ticker, name := range tickers {
		log.Printf("[%s] Обработка...", ticker)
		atr, err := getATR(accessToken, ticker)
		if err != nil {
			log.Printf("  ⚠️ Ошибка ATR [%s]: %v", ticker, err)
			continue
		}
		_, err = db.Exec(
			`INSERT INTO atr_values (ticker, name, atr_value, period, timeframe)
			 VALUES ($1, $2, $3, $4, $5)`,
			ticker, name, atr, ATR_PERIOD, TIMEFRAME,
		)
		if err != nil {
			log.Printf("  ⚠️ Ошибка записи в БД: %v", err)
			continue
		}
		log.Printf("  ✅ %s: ATR = %.6f", ticker, atr)
		successCount++
	}

		// ГО и рыночные данные из API Мосбиржи
	for ticker := range tickers {
		spec, err := moex.GetSpec(ticker)
		if err != nil {
			log.Printf("⚠️ Ошибка получения данных Мосбиржи для %s: %v", ticker, err)
			continue
		}
		_, err = db.Exec(
    `UPDATE instrument_specs SET 
     go_buy=$1, go_sell=$2, lot_size=$3, last_price=$4, spread=$5, volume_day=$6, 
     open_price=$7, high_price=$8, low_price=$9, prev_close=$10, num_trades=$11, 
     updated_at=NOW() 
     WHERE ticker=$12`,
    spec.GoBuy, spec.GoSell, spec.LotSize, spec.LastPrice, spec.Spread, spec.VolumeDay,
    spec.OpenPrice, spec.HighPrice, spec.LowPrice, spec.PrevClose, spec.NumTrades,
    ticker,
)
		if err != nil {
			log.Printf("⚠️ Ошибка сохранения данных для %s: %v", ticker, err)
		} else {
			log.Printf("📋 %s: ГО=%.2f, Цена=%.2f, Спред=%.4f, Объём=%.0f ₽, Сделок=%d, Экспирация=%s",
				ticker, spec.GoBuy, spec.LastPrice, spec.Spread, spec.VolumeDay, spec.NumTrades, spec.MaturityDate)
		}
	}

	log.Printf("=== Готово! Обработано %d из %d ===", successCount, len(tickers))
}

func getAccessToken(refreshToken string) (string, string, error) {
	formData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"trade-api-write"},
	}
	req, err := http.NewRequest("POST", BSC_TOKEN_URL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", "", fmt.Errorf("ошибка парсинга токена: %w", err)
	}
	return tokenResp.AccessToken, tokenResp.RefreshToken, nil
}

func getActiveTickers(db *sql.DB) (map[string]string, error) {
    rows, err := db.Query("SELECT DISTINCT ticker, ticker FROM user_instruments")
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    tickers := make(map[string]string)
    for rows.Next() {
        var ticker, name string
        rows.Scan(&ticker, &name)
        tickers[ticker] = ticker
    }
    return tickers, nil
}

func getATR(accessToken, ticker string) (float64, error) {
	now := time.Now().UTC()
	from := now.Add(-14 * 24 * time.Hour)
	req, err := http.NewRequest("GET", BSC_CANDLES_URL, nil)
	if err != nil {
		return 0, err
	}
	q := req.URL.Query()
	q.Add("classCode", CLASS_CODE)
	q.Add("ticker", ticker)
	q.Add("startDate", from.Format("2006-01-02T15:04:05Z"))
	q.Add("endDate", now.Format("2006-01-02T15:04:05Z"))
	q.Add("timeFrame", TIMEFRAME)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var candlesResp CandlesResponse
	if err := json.Unmarshal(body, &candlesResp); err != nil {
		return 0, fmt.Errorf("ошибка парсинга: %w", err)
	}
	bars := candlesResp.Bars
	if len(bars) < ATR_PERIOD+1 {
		return 0, fmt.Errorf("недостаточно свечей: нужно %d, получено %d", ATR_PERIOD+1, len(bars))
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Time < bars[j].Time })
	trValues := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		high := bars[i].High
		low := bars[i].Low
		prevClose := bars[i-1].Close
		tr := math.Max(high-low, math.Max(math.Abs(high-prevClose), math.Abs(low-prevClose)))
		trValues = append(trValues, tr)
	}
	sum := 0.0
	start := len(trValues) - ATR_PERIOD
	for i := start; i < len(trValues); i++ {
		sum += trValues[i]
	}
	return sum / float64(ATR_PERIOD), nil
}

func getSpecs(accessToken string, tickers []string) (map[string]SpecInfo, error) {
	bodyData := map[string][]string{"tickers": tickers}
	bodyJSON, _ := json.Marshal(bodyData)
	req, err := http.NewRequest("POST", BSC_SPEC_URL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
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
	var specs []struct {
		Ticker      string  `json:"ticker"`
		MinimumStep float64 `json:"minimumStep"`
		StepPrice   float64 `json:"stepPrice"`
		LotSize     float64 `json:"lotSize"`
	}
	if err := json.Unmarshal(body, &specs); err != nil {
		return nil, fmt.Errorf("ошибка парсинга: %w", err)
	}
	result := make(map[string]SpecInfo)
	for _, s := range specs {
		result[s.Ticker] = SpecInfo{
			Step:      s.MinimumStep,
			StepPrice: s.StepPrice,
			LotSize:   s.LotSize,
		}
	}
	return result, nil
}

func getMoexSpec(ticker string) (goBuy, goSell, lotSize, lastPrice, spread, volumeDay, openPrice, highPrice, lowPrice, prevClose float64, numTrades int, maturityDate string, err error) {
	url := fmt.Sprintf("https://iss.moex.com/iss/engines/futures/markets/forts/securities/%s.json", ticker)
	req, _ := http.NewRequest("GET", url, nil)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

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
	json.Unmarshal(body, &result)

	if len(result.Securities.Data) > 0 {
		row := result.Securities.Data[0]
		for i, col := range result.Securities.Columns {
			switch col {
			case "INITIALMARGIN":
				if v, ok := row[i].(float64); ok { goBuy, goSell = v, v }
			case "LOTVOLUME":
				if v, ok := row[i].(float64); ok { lotSize = v }
			case "LASTTRADEDATE":
				if v, ok := row[i].(string); ok { maturityDate = v }
			}
		}
	}

	if len(result.Marketdata.Data) > 0 {
		row := result.Marketdata.Data[0]
		for i, col := range result.Marketdata.Columns {
			switch col {
			case "LAST":
				if v, ok := row[i].(float64); ok { lastPrice = v }
			case "SPREAD":
				if v, ok := row[i].(float64); ok { spread = v }
			case "VALTODAY":
				if v, ok := row[i].(float64); ok { volumeDay = v }
			case "NUMTRADES":
				if v, ok := row[i].(float64); ok { numTrades = int(v) }
			case "OPEN":
				if v, ok := row[i].(float64); ok { openPrice = v }
			case "HIGH":
				if v, ok := row[i].(float64); ok { highPrice = v }
			case "LOW":
				if v, ok := row[i].(float64); ok { lowPrice = v }
			case "SETTLEPRICE":
				if v, ok := row[i].(float64); ok { prevClose = v }
			}
		}
	}

	return goBuy, goSell, lotSize, lastPrice, spread, volumeDay, openPrice, highPrice, lowPrice, prevClose, numTrades, maturityDate, nil
}


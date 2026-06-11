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

	_ "github.com/lib/pq"
)

const (
	BSC_TOKEN_URL   = "https://be.broker.ru/trade-api-keycloak/realms/tradeapi/protocol/openid-connect/token"
	BSC_CANDLES_URL = "https://be.broker.ru/trade-api-market-data-connector/api/v1/candles-chart"
	ATR_PERIOD      = 10
	CLASS_CODE      = "SPBFUT"
	TIMEFRAME       = "H4"
	TOKEN_FILE      = "refresh_token.txt" // Файл для сохранения токена
)

var NEON_DATABASE_URL = os.Getenv("NEON_DATABASE_URL")

var tickers = map[string]string{
	"BRU6": "Brent",
	"EDU6": "Eurodollar",
	"RIU6": "RTS",
	"SRU6": "Сбербанк",
	"EuU6": "Евро/Рубль",
	"GZU6": "Газпром",
	"MMU6": "Мосбиржа",
	"MNU6": "Норникель",
	"LKU6": "Лукойл",
	"SiU6": "Доллар/Рубль",
	"VBU6": "ВТБ",
	"SVU6": "Сбербанк-преф",
	"GDU6": "Золото",
	"GKU6": "Медь",
	"PDU6": "Палладий",
	"NGU6": "Газ США",
	"SNU6": "Олово",
	"YDU6": "Юань/Рубль",
	"SFU6": "Швейцарский франк",
	"CRU6": "Хром",
	"PHU6": "Фосфор",
}

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

func main() {
	log.Println("=== Сборщик ATR (БКС REST API) ===")

	if NEON_DATABASE_URL == "" {
		log.Fatal("❌ NEON_DATABASE_URL не задан")
	}

	// Читаем refresh-токен из файла или переменной
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

	// Получаем access-токен (и новый refresh-токен)
	accessToken, newRefreshToken, err := getAccessToken(refreshToken)
	if err != nil {
		log.Fatal("❌ Ошибка получения access token:", err)
	}
	log.Println("✅ Access token получен")

	// Сохраняем новый refresh-токен
	if newRefreshToken != "" && newRefreshToken != refreshToken {
		if err := os.WriteFile(TOKEN_FILE, []byte(newRefreshToken), 0600); err != nil {
			log.Printf("⚠️ Не удалось сохранить refresh-токен: %v", err)
		} else {
			log.Println("✅ Refresh-токен обновлён и сохранён")
		}
	}

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

func getATR(accessToken, ticker string) (float64, error) {
	now := time.Now().UTC()
	from := now.Add(-5 * 24 * time.Hour)

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

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Time < bars[j].Time
	})

	trValues := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		high := bars[i].High
		low := bars[i].Low
		prevClose := bars[i-1].Close

		tr := math.Max(
			high-low,
			math.Max(
				math.Abs(high-prevClose),
				math.Abs(low-prevClose),
			),
		)
		trValues = append(trValues, tr)
	}

	sum := 0.0
	start := len(trValues) - ATR_PERIOD
	for i := start; i < len(trValues); i++ {
		sum += trValues[i]
	}

	return sum / float64(ATR_PERIOD), nil
}
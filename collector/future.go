package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Future — структура данных о фьючерсе
type Future struct {
	Ticker      string
	BaseCode    string
	Type        string
	Description string
	ShortCode   string
	Maturity    time.Time
	Step        float64
	StepPrice   float64
	LotSize     float64
	GoBuy       float64
	GoSell      float64
	LastPrice   float64
	Spread      float64
	VolumeDay   float64
	OpenPrice   float64
	HighPrice   float64
	LowPrice    float64
	PrevClose   float64
	NumTrades   int
	ATR         float64
}

// LoadMoex загружает данные фьючерса из API Мосбиржи
func (f *Future) LoadMoex() error {
	url := fmt.Sprintf("https://iss.moex.com/iss/engines/futures/markets/forts/securities/%s.json", f.Ticker)
	resp, err := http.Get(url)
	if err != nil {
		return err
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

	// Из securities
	if len(result.Securities.Data) > 0 {
		row := result.Securities.Data[0]
		for i, col := range result.Securities.Columns {
			switch col {
			case "INITIALMARGIN":
				if v, ok := row[i].(float64); ok {
					f.GoBuy, f.GoSell = v, v
				}
			case "LOTVOLUME":
				if v, ok := row[i].(float64); ok {
					f.LotSize = v
				}
			case "LASTTRADEDATE":
				if v, ok := row[i].(string); ok {
					f.Maturity, _ = time.Parse("2006-01-02", v)
				}
			case "ASSETCODE":
				if v, ok := row[i].(string); ok {
					f.BaseCode = v
				}
			case "SECNAME":
				if v, ok := row[i].(string); ok {
					f.Description = v
				}
			case "TYPE":
				if v, ok := row[i].(string); ok {
					f.Type = v
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
					f.LastPrice = v
				}
			case "SPREAD":
				if v, ok := row[i].(float64); ok {
					f.Spread = v
				}
			case "VALTODAY":
				if v, ok := row[i].(float64); ok {
					f.VolumeDay = v
				}
			case "NUMTRADES":
				if v, ok := row[i].(float64); ok {
					f.NumTrades = int(v)
				}
			case "OPEN":
				if v, ok := row[i].(float64); ok {
					f.OpenPrice = v
				}
			case "HIGH":
				if v, ok := row[i].(float64); ok {
					f.HighPrice = v
				}
			case "LOW":
				if v, ok := row[i].(float64); ok {
					f.LowPrice = v
				}
			case "SETTLEPRICE":
				if v, ok := row[i].(float64); ok {
					f.PrevClose = v
				}
			}
		}
	}

	f.ShortCode = extractPrefix(f.Ticker)
	return nil
}

// Prefix возвращает короткий код тикера (BRM6 → BR)
func (f *Future) Prefix() string {
	if f.ShortCode != "" {
		return f.ShortCode
	}
	return extractPrefix(f.Ticker)
}

// IsActive проверяет, не истёк ли фьючерс
func (f *Future) IsActive() bool {
	if f.Maturity.IsZero() {
		return true
	}
	return f.Maturity.After(time.Now())
}

// extractPrefix извлекает буквенную часть тикера
func extractPrefix(ticker string) string {
	letters := ""
	for _, c := range ticker {
		if c >= 'A' && c <= 'Z' {
			letters += string(c)
		} else {
			break
		}
	}
	if len(letters) > 2 {
		last := letters[len(letters)-1]
		if strings.ContainsRune("FGHJKMNQUVXZ", rune(last)) {
			letters = letters[:len(letters)-1]
		}
	}
	return letters
}

// LoadBcs загружает спецификации из API БКС (шаг, стоимость шага, лот)
func (f *Future) LoadBcs(accessToken string) error {
	bodyData := map[string][]string{"tickers": []string{f.Ticker}}
	bodyJSON, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", BSC_SPEC_URL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var specs []struct {
		Ticker      string  `json:"ticker"`
		MinimumStep float64 `json:"minimumStep"`
		StepPrice   float64 `json:"stepPrice"`
		LotSize     float64 `json:"lotSize"`
	}
	if err := json.Unmarshal(body, &specs); err != nil {
		return fmt.Errorf("ошибка парсинга спецификаций: %w", err)
	}

	for _, s := range specs {
		if s.Ticker == f.Ticker {
			f.Step = s.MinimumStep
			f.StepPrice = s.StepPrice
			f.LotSize = s.LotSize
			return nil
		}
	}
	return fmt.Errorf("тикер %s не найден в ответе БКС", f.Ticker)
}

// Save сохраняет данные фьючерса в базу данных
func (f *Future) Save(db *sql.DB) error {
	// Сохраняем в instrument_specs
	_, err := db.Exec(`
		INSERT INTO instrument_specs (ticker, step, step_price, lot_size, go_buy, go_sell, last_price, spread, volume_day,
			open_price, high_price, low_price, prev_close, num_trades, maturity_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (ticker) DO UPDATE SET
			step=$2, step_price=$3, lot_size=$4, go_buy=$5, go_sell=$6, last_price=$7, spread=$8, volume_day=$9,
			open_price=$10, high_price=$11, low_price=$12, prev_close=$13, num_trades=$14, maturity_date=$15, updated_at=NOW()`,
		f.Ticker, f.Step, f.StepPrice, f.LotSize, f.GoBuy, f.GoSell, f.LastPrice, f.Spread, f.VolumeDay,
		f.OpenPrice, f.HighPrice, f.LowPrice, f.PrevClose, f.NumTrades, f.Maturity,
	)
	if err != nil {
		return fmt.Errorf("ошибка сохранения спецификаций: %w", err)
	}

	// Сохраняем в atr_values
	_, err = db.Exec(`
		INSERT INTO atr_values (ticker, atr_value, period, timeframe)
		VALUES ($1, $2, $3, $4)`,
		f.Ticker, f.ATR, ATR_PERIOD, TIMEFRAME,
	)
	if err != nil {
		return fmt.Errorf("ошибка сохранения ATR: %w", err)
	}

	return nil
}

// DiscoverNew находит новые фьючерсы через API Мосбиржи и добавляет их в futures_guide
func DiscoverNew(db *sql.DB) (int, error) {
	url := "https://iss.moex.com/iss/engines/futures/markets/forts/securities.json?limit=500"
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Securities struct {
			Data    [][]interface{} `json:"data"`
			Columns []string        `json:"columns"`
		} `json:"securities"`
	}
	json.Unmarshal(body, &result)

	colIdx := make(map[string]int)
	for i, col := range result.Securities.Columns {
		colIdx[col] = i
	}

	newCount := 0
	for _, row := range result.Securities.Data {
		ticker := getString(row, colIdx, "SECID")
		sectype := getString(row, colIdx, "SECTYPE")
		name := getString(row, colIdx, "SECNAME")

		if ticker == "" || sectype == "" {
			continue
		}

		// Проверяем, есть ли уже в futures_guide
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM futures_guide WHERE ticker=$1)", ticker).Scan(&exists)
		if exists {
			continue
		}

		// Добавляем новый
		f := &Future{Ticker: ticker}
		f.LoadMoex() // Загружаем полные данные ( maturity)
		
		_, err := db.Exec(`INSERT INTO futures_guide (ticker, base_code, type, description, short_code, maturity_date, is_active)
			VALUES ($1, $2, 'futures', $3, $4, $5, $6)`,
			f.Ticker, f.BaseCode, f.Description, f.Prefix(), f.Maturity, f.IsActive())
		if err != nil {
			log.Printf("Ошибка добавления %s: %v", ticker, err)
		} else {
			newCount++
		}
	}
	return newCount, nil
}

// StatusMessage возвращает понятный статус для пользователя
func (f *Future) StatusMessage() string {
    if !f.IsActive() {
        return "Истёк"
    }
    if f.LastPrice > 0 || f.VolumeDay > 0 || f.NumTrades > 0 {
        return "Торгуется"
    }
    return "Нет торгов"
}
// определяет торгуется ли фьючерс
func (f *Future) IsTrading() bool {
    return f.LastPrice > 0 || f.VolumeDay > 0 || f.NumTrades > 0
}
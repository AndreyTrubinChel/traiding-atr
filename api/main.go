package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"trading-atr/pkg/future"

	_ "github.com/lib/pq"
)

type Future struct {
    Ticker    string
    LastPrice float64
    VolumeDay float64
    NumTrades int
}

func (f *Future) LoadMoex() error {
    url := fmt.Sprintf("https://iss.moex.com/iss/engines/futures/markets/forts/securities/%s.json", f.Ticker)
    resp, err := http.Get(url)
    if err != nil { return err }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    
    var result struct {
        Marketdata struct {
            Data    [][]interface{} `json:"data"`
            Columns []string        `json:"columns"`
        } `json:"marketdata"`
    }
    json.Unmarshal(body, &result)
    
    if len(result.Marketdata.Data) > 0 {
        row := result.Marketdata.Data[0]
        for i, col := range result.Marketdata.Columns {
            switch col {
            case "LAST": if v, ok := row[i].(float64); ok { f.LastPrice = v }
            case "VALTODAY": if v, ok := row[i].(float64); ok { f.VolumeDay = v }
            case "NUMTRADES": if v, ok := row[i].(float64); ok { f.NumTrades = int(v) }
            }
        }
    }
    return nil
}

func (f *Future) IsTrading() bool {
    return f.LastPrice > 0 || f.VolumeDay > 0 || f.NumTrades > 0
}

var db *sql.DB
var prefixMap map[string]string

func main() {
	NEON_DATABASE_URL := os.Getenv("NEON_DATABASE_URL")
	if NEON_DATABASE_URL == "" {
		log.Fatal("❌ NEON_DATABASE_URL не задан")
	}

	var err error
	db, err = sql.Open("postgres", NEON_DATABASE_URL)
	if err != nil {
		log.Fatal("❌ Ошибка подключения к БД:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("❌ БД не отвечает:", err)
	}
	log.Println("✅ Подключено к Neon")

	log.Println("DEBUG: перед loadPrefixMap")

	loadPrefixMap()

	http.HandleFunc("/api/atr/latest", handleLatest)
	http.HandleFunc("/api/atr/csv", handleCSV)
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/api/instruments/search", handleSearch)
	http.HandleFunc("/api/instruments/add", handleAddInstrument)
	http.HandleFunc("/api/user/register", handleRegister)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 API запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))

	

}


func loadPrefixMap() {

	log.Println("🔄 Загрузка prefix_map.json...")

    data, err := os.ReadFile("../collector/prefix_map.json")
    if err != nil {
        log.Printf("⚠️ Не удалось загрузить prefix_map.json: %v", err)
        prefixMap = make(map[string]string)
        return
    }
    json.Unmarshal(data, &prefixMap)
    log.Printf("📋 Загружено %d префиксов", len(prefixMap))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func handleLatest(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT ticker, atr_value, period, timeframe, calculated_at FROM atr_latest ORDER BY ticker`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ATRItem struct {
		Ticker       string    `json:"ticker"`
		ATRValue     float64   `json:"atr_value"`
		Period       int       `json:"period"`
		Timeframe    string    `json:"timeframe"`
		CalculatedAt time.Time `json:"calculated_at"`
	}
	var result []ATRItem
	for rows.Next() {
		var item ATRItem
		if err := rows.Scan(&item.Ticker, &item.ATRValue, &item.Period, &item.Timeframe, &item.CalculatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result = append(result, item)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(result)
}

func handleCSV(w http.ResponseWriter, r *http.Request) {
	 
	log.Printf(">>> handleCSV вызван <<<")

	userID := r.URL.Query().Get("user")
	if userID == "" {
		userID = "test"
	}
	log.Printf("CSV запрос: user=%s", userID)

	rows, err := db.Query(`
		SELECT 
			COALESCE(ui.base_code, '') as base_code,
			COALESCE(ui.display_name, ba.display_name, ui.ticker) as display_name,
			ui.ticker,
			COALESCE(TO_CHAR(fg.maturity_date, 'DD.MM.YYYY'), '') as maturity_date,
			'Торгуется' as status,
			COALESCE(s.step, 0) as step,
			COALESCE(s.step_price, 0) as step_price,
			COALESCE(s.lot_size, 0) as lot_size,
			COALESCE(s.go_buy, 0) as go_buy,
			COALESCE(s.go_sell, 0) as go_sell,
			COALESCE(s.last_price, 0) as last_price,
			COALESCE(s.open_price, 0) as open_price,
			COALESCE(s.high_price, 0) as high_price,
			COALESCE(s.low_price, 0) as low_price,
			COALESCE(s.prev_close, 0) as prev_close,
			COALESCE(s.spread, 0) as spread,
			COALESCE(s.volume_day, 0) as volume_day,
			COALESCE(s.num_trades, 0) as num_trades,
			COALESCE(a.atr_value, 0) as atr_value
		FROM user_instruments ui
		LEFT JOIN atr_latest a ON ui.ticker = a.ticker
		LEFT JOIN instrument_specs s ON ui.ticker = s.ticker
		LEFT JOIN futures_guide fg ON ui.ticker = fg.ticker
		LEFT JOIN base_assets ba ON ui.base_code = ba.base_code
		WHERE ui.user_id = $1
		AND (fg.maturity_date IS NULL OR fg.maturity_date IS NULL OR fg.maturity_date > CURRENT_DATE)
		ORDER BY ui.base_code
	`, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	fmt.Fprint(w, "Баз.актив\tНазвание\tТикер\tЭкспирация\tДней до эксп.\tСтатус\tШаг\tСтоимость шага\tЛот\tГО покупки\tГО продажи\tЦена\tOPEN\tHIGH\tLOW\tPrevClose\tСпред\tОбъём за день\tСделок\tATR\n")

	for rows.Next() {
		var baseCode, displayName, ticker, maturityDate, status string
		var step, stepPrice, lotSize, goBuy, goSell, lastPrice, openPrice, highPrice, lowPrice, prevClose, spread, volumeDay, atr float64
		var numTrades int
		if err := rows.Scan(&baseCode, &displayName, &ticker, &maturityDate, &status, &step, &stepPrice, &lotSize, &goBuy, &goSell, &lastPrice, &openPrice, &highPrice, &lowPrice, &prevClose, &spread, &volumeDay, &numTrades, &atr); err != nil {
    log.Printf("Ошибка сканирования CSV: %v", err)
    continue
}

		daysToExpiry := ""
		if maturityDate != "" {
			if t, err := time.Parse("02.01.2006", maturityDate); err == nil {
				days := int(t.Sub(time.Now()).Hours() / 24)
				daysToExpiry = fmt.Sprintf("%d", days)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			baseCode, displayName, ticker, maturityDate, daysToExpiry, status,
			strings.Replace(fmt.Sprintf("%.4f", step), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", stepPrice), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.0f", lotSize), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", goBuy), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", goSell), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", lastPrice), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", openPrice), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", highPrice), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", lowPrice), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", prevClose), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.2f", spread), ".", ",", 1),
			strings.Replace(fmt.Sprintf("%.0f", volumeDay), ".", ",", 1),
			numTrades,
			strings.Replace(fmt.Sprintf("%.10f", atr), ".", ",", 1),
		)
	}

}


func handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}
	url := fmt.Sprintf("https://iss.moex.com/iss/securities.json?q=%s&engine=futures&market=forts&limit=30", q)
	resp, err := http.Get(url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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

	type SearchItem struct {
		Ticker string `json:"ticker"`
		Name   string `json:"name"`
	}
	var items []SearchItem
	colIdx := make(map[string]int)
	for i, col := range result.Securities.Columns {
		colIdx[col] = i
	}
	for _, row := range result.Securities.Data {
		ticker, name, secType, group := "", "", "", ""
		if idx, ok := colIdx["secid"]; ok {
			if v, ok := row[idx].(string); ok { ticker = v }
		}
		if idx, ok := colIdx["shortname"]; ok {
			if v, ok := row[idx].(string); ok { name = v }
		}
		if idx, ok := colIdx["type"]; ok {
			if v, ok := row[idx].(string); ok { secType = v }
		}
		if idx, ok := colIdx["group"]; ok {
			if v, ok := row[idx].(string); ok { group = v }
		}
		if ticker != "" && secType == "futures" && group == "futures_forts" {
			items = append(items, SearchItem{Ticker: ticker, Name: name})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(items)
}

func handleAddInstrument(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Ticker string `json:"ticker"`
		UserID string `json:"user_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Ticker == "" {
		http.Error(w, "ticker required", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		req.UserID = "test"
	}
	_, err := db.Exec(`INSERT INTO user_instruments (user_id, ticker) VALUES ($1, $2) ON CONFLICT (user_id, ticker) DO NOTHING`, req.UserID, req.Ticker)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "ticker": req.Ticker})
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	log.Printf("Запрос регистрации: %s", r.Method)
	var req struct {
		UserID string `json:"user_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.UserID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM user_instruments WHERE user_id = $1", req.UserID).Scan(&count)
	if count > 0 {
		log.Printf("Регистрация: %s (уже существует)", req.UserID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "exists", "user_id": req.UserID})
		return
	}
	rows, err := db.Query("SELECT base_code, display_name FROM default_assets")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type asset struct {
		baseCode    string
		displayName string
	}
	var assets []asset
	for rows.Next() {
		var a asset
		rows.Scan(&a.baseCode, &a.displayName)
		assets = append(assets, a)
	}
	for _, a := range assets {
		ticker := findActiveTicker(a.baseCode)

		log.Printf("  DEBUG: %s -> %s", a.baseCode, ticker)

		if ticker == "" {
			ticker = a.baseCode
		}
		db.Exec(`INSERT INTO user_instruments (user_id, base_code, display_name, ticker) VALUES ($1, $2, $3, $4)`, req.UserID, a.baseCode, a.displayName, ticker)
	}
	log.Printf("Регистрация: %s (добавлено %d инструментов)", req.UserID, len(assets))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "user_id": req.UserID})
}

func findActiveTicker(baseCode string) string {
	searchPrefix := baseCode
	if p, ok := prefixMap[baseCode]; ok {
		searchPrefix = p
	}
	log.Printf("findActiveTicker: base=%s, prefix=%s", baseCode, searchPrefix)

	url := fmt.Sprintf("https://iss.moex.com/iss/securities.json?q=%s-&engine=futures&market=forts&limit=10", searchPrefix)
	resp, err := http.Get(url)
	if err != nil {
		return ""
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
	for _, row := range result.Securities.Data {
		ticker, secType := "", ""
		if idx, ok := colIdx["secid"]; ok {
			if v, ok := row[idx].(string); ok { ticker = v }
		}
		if idx, ok := colIdx["type"]; ok {
			if v, ok := row[idx].(string); ok { secType = v }
		}
		if ticker != "" && secType == "futures" && strings.HasPrefix(ticker, searchPrefix) {
    	d := &future.Derivative{Ticker: ticker}
    	if err := d.LoadFromMoex(); err == nil && !d.IsExpired() {
        	log.Printf("findActiveTicker: НАЙДЕН %s для %s", ticker, baseCode)
        return ticker
    }
}
}
	log.Printf("findActiveTicker: НЕ НАЙДЕН для %s", baseCode)
	return ""
}


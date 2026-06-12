package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

type ATRItem struct {
	Ticker       string    `json:"ticker"`
	Name         string    `json:"name"`
	ATRValue     float64   `json:"atr_value"`
	Period       int       `json:"period"`
	Timeframe    string    `json:"timeframe"`
	CalculatedAt time.Time `json:"calculated_at"`
}

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

	http.HandleFunc("/api/atr/latest", handleLatest)
	http.HandleFunc("/api/atr/csv", handleCSV)
	http.HandleFunc("/health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 API запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// GET /api/atr/latest — JSON
func handleLatest(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT ticker, name, atr_value, period, timeframe, calculated_at
		FROM atr_latest
		ORDER BY ticker
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []ATRItem
	for rows.Next() {
		var item ATRItem
		if err := rows.Scan(&item.Ticker, &item.Name, &item.ATRValue, &item.Period, &item.Timeframe, &item.CalculatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result = append(result, item)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(result)
}

// GET /api/atr/csv — CSV в формате "Инструмент\tЗначение"
func handleCSV(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT 
        COALESCE(ai.base_code, '') as base_code,
        COALESCE(ba.display_name, a.name) as display_name,
        a.ticker,
        COALESCE(TO_CHAR(s.maturity_date, 'DD.MM.YYYY'), '') as maturity_date,
        CASE WHEN COALESCE(s.last_price, 0) > 0 OR COALESCE(s.volume_day, 0) > 0 OR COALESCE(s.num_trades, 0) > 0 
            THEN 'Торгуется' 
            ELSE 'Нет торгов' 
        END as status,
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
        a.atr_value
    FROM atr_latest a
    LEFT JOIN instrument_specs s ON a.ticker = s.ticker
    LEFT JOIN active_instruments ai ON a.ticker = ai.ticker
    LEFT JOIN base_assets ba ON ai.base_code = ba.base_code
    ORDER BY a.ticker
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte{0xEF, 0xBB, 0xBF})

fmt.Fprint(w, "Инструмент\tНазвание\tБаз.актив\tТикер\tЭкспирация\tДней до эксп.\tСтатус\tШаг\tСтоимость шага\tЛот\tГО покупки\tГО продажи\tЦена\tOPEN\tHIGH\tLOW\tPrevClose\tСпред\tОбъём за день\tСделок\tATR\n")

	for rows.Next() {
		var baseCode, displayName, ticker, maturityDate, status string
var step, stepPrice, lotSize, goBuy, goSell, lastPrice, openPrice, highPrice, lowPrice, prevClose, spread, volumeDay, atr float64
var numTrades int
if err := rows.Scan(&baseCode, &displayName, &ticker, &maturityDate, &status, &step, &stepPrice, &lotSize, &goBuy, &goSell, &lastPrice, &openPrice, &highPrice, &lowPrice, &prevClose, &spread, &volumeDay, &numTrades, &atr); err != nil {
    continue
}

		shortTicker := strings.TrimSuffix(ticker, "U6")
daysToExpiry := ""
if maturityDate != "" {
    if t, err := time.Parse("02.01.2006", maturityDate); err == nil {
        days := int(t.Sub(time.Now()).Hours() / 24)
        daysToExpiry = fmt.Sprintf("%d", days)
    }
}

fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
    shortTicker, displayName, baseCode, ticker,
    maturityDate, daysToExpiry, status,
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
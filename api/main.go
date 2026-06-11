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
		              SELECT a.ticker, 
           COALESCE(ba.display_name, a.name) as display_name,
           COALESCE(ai.base_code, '') as base_code,
           a.atr_value, 
           COALESCE(s.step, 0), 
           COALESCE(s.step_price, 0), 
           COALESCE(s.lot_size, 0),
           COALESCE(s.go_buy, 0),
           COALESCE(s.go_sell, 0),
           COALESCE(s.last_price, 0),
           COALESCE(s.spread, 0),
           COALESCE(s.volume_day, 0),
           COALESCE(s.num_trades, 0)
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

	fmt.Fprint(w, "Инструмент\tНазвание\tБаз.актив\tATR\tШаг\tСтоимость шага\tЛот\tГО покупки\tГО продажи\tЦена\tСпред\tОбъём за день\tСделок\n")

	for rows.Next() {
	var ticker, displayName, baseCode string
	var atr, step, stepPrice, lotSize, goBuy, goSell, lastPrice, spread, volumeDay float64
	var numTrades int
	if err := rows.Scan(&ticker, &	displayName, &baseCode, &atr, &step, &stepPrice, &lotSize, &goBuy, &goSell, &lastPrice, &spread, &volumeDay, &numTrades); err != nil {
		continue
	}
shortTicker := strings.TrimSuffix(ticker, "U6")
fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
    shortTicker, displayName, baseCode,
    strings.Replace(fmt.Sprintf("%.10f", atr), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.4f", step), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.2f", stepPrice), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.0f", lotSize), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.2f", goBuy), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.2f", goSell), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.2f", lastPrice), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.2f", spread), ".", ",", 1),
    strings.Replace(fmt.Sprintf("%.0f", volumeDay), ".", ",", 1),
    numTrades,
	)
	
	}
}
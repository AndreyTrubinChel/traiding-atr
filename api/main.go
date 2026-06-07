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
		SELECT ticker, atr_value
		FROM atr_latest
		ORDER BY ticker
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// BOM для Excel (чтобы правильно открывал UTF-8)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	// BOM — специальный символ в начале файла
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	// Заголовок
	fmt.Fprint(w, "Инструмент\tЗначение\n")

	for rows.Next() {
		var ticker string
		var atr float64
		if err := rows.Scan(&ticker, &atr); err != nil {
			continue
		}
		shortTicker := strings.TrimSuffix(ticker, "U6")
		atrStr := strings.Replace(fmt.Sprintf("%.10f", atr), ".", ",", 1)
		fmt.Fprintf(w, "%s\t%s\n", shortTicker, atrStr)
	}
}
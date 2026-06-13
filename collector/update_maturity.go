package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
)

var NEON_DATABASE_URL = os.Getenv("NEON_DATABASE_URL")

func main() {
	db, err := sql.Open("postgres", NEON_DATABASE_URL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Println("Начинаю обновление дат...")
	
	rows, err := db.Query("SELECT ticker FROM futures_guide WHERE maturity_date IS NULL")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var ticker string
		rows.Scan(&ticker)

		url := fmt.Sprintf("https://iss.moex.com/iss/engines/futures/markets/forts/securities/%s.json", ticker)
		resp, _ := http.Get(url)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			Securities struct {
				Data    [][]interface{} `json:"data"`
				Columns []string        `json:"columns"`
			} `json:"securities"`
		}
		json.Unmarshal(body, &result)

		if len(result.Securities.Data) > 0 {
			for i, col := range result.Securities.Columns {
				if col == "LASTTRADEDATE" {
					if v, ok := result.Securities.Data[0][i].(string); ok && v != "" {
						t, _ := time.Parse("2006-01-02", v)
						db.Exec("UPDATE futures_guide SET maturity_date=$1 WHERE ticker=$2", t, ticker)
						count++
					}
				}
			}
		}
		if count%50 == 0 && count > 0 {
			fmt.Printf("Обработано %d...\n", count)
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("Обновлено %d записей\n", count)
}
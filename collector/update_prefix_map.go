package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

var monthCodes = map[string]bool{
	"F": true, "G": true, "H": true, "J": true, "K": true,
	"M": true, "N": true, "Q": true, "U": true, "V": true,
	"X": true, "Z": true,
}

func main() {
	url := "https://iss.moex.com/iss/engines/futures/markets/forts/securities.json?limit=500"
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("Ошибка запроса:", err)
		os.Exit(1)
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

	secidIdx := -1
	assetIdx := -1
	for i, col := range result.Securities.Columns {
		switch col {
		case "SECID":
			secidIdx = i
		case "ASSETCODE":
			assetIdx = i
		}
	}

	// Собираем все тикеры для каждого ASSETCODE
	assetTickers := make(map[string][]string)
	for _, row := range result.Securities.Data {
		if len(row) <= secidIdx || len(row) <= assetIdx {
			continue
		}
		secid, _ := row[secidIdx].(string)
		asset, _ := row[assetIdx].(string)
		if asset == "" || secid == "" {
			continue
		}
		assetTickers[asset] = append(assetTickers[asset], secid)
	}

	// Строим маппинг: ASSETCODE → общий префикс
	prefixMap := make(map[string]string)
	re := regexp.MustCompile(`^([A-Za-z]+)`)

	for asset, tickers := range assetTickers {
		if len(tickers) == 0 {
			continue
		}
		// Берём самый короткий тикер и обрезаем код месяца на конце
		shortest := tickers[0]
		for _, t := range tickers {
			if len(t) < len(shortest) {
				shortest = t
			}
		}
		// Извлекаем буквенную часть
		match := re.FindString(shortest)
		if match != "" {
			// Если последняя буква — код месяца, убираем её
			last := strings.ToUpper(match[len(match)-1:])
			if monthCodes[last] && len(match) > 1 {
				match = match[:len(match)-1]
			}
			prefixMap[asset] = match
		}
	}

	data, _ := json.MarshalIndent(prefixMap, "", "  ")
	os.WriteFile("prefix_map.json", data, 0644)
	fmt.Printf("Сохранено %d активов в prefix_map.json\n", len(prefixMap))
}
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	bcstrade "github.com/tigusigalpa/bcs-trade-go"
	"github.com/tigusigalpa/bcs-trade-go/models"
)

func main() {
	ctx := context.Background()

	client, err := bcstrade.NewFromEnv(ctx)
	if err != nil {
		log.Fatal("Ошибка создания клиента:", err)
	}

	// Тест 1: Спецификация
	fmt.Println("=== Тест 1: Спецификация BRU6 ===")
	inst, err := client.Information.GetInstrumentByTicker(ctx, "SPBFUT", "BRU6")
	if err != nil {
		log.Printf("Ошибка спецификации: %v", err)
	} else {
		fmt.Printf("Тикер: %s\n", inst.Ticker)
		fmt.Printf("Шаг цены: %f\n", inst.MinStep)
		fmt.Printf("Стоимость шага: %f\n", inst.StepPrice)
		fmt.Printf("Лот: %f\n", inst.LotSize)
	}

	// Тест 2: Свечи 4H
	fmt.Println("\n=== Тест 2: Свечи BRU6 (4H) ===")
	candles, err := client.MarketData.GetCandles(ctx, models.GetCandlesParams{
		ClassCode: "SPBFUT",
		Ticker:    "BRU6",
		StartDate: time.Now().Add(-5 * 24 * time.Hour),
		EndDate:   time.Now(),
		TimeFrame: models.TimeFrameH4,
	})
	if err != nil {
		log.Printf("Ошибка свечей: %v", err)
	} else {
		fmt.Printf("Получено свечей: %d\n", len(candles.Bars))
		if len(candles.Bars) > 0 {
			last := candles.Bars[len(candles.Bars)-1]
			fmt.Printf("Последняя цена закрытия: %f\n", last.Close)
		}
	}

	fmt.Println("\n=== Готово ===")
}
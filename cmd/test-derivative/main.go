package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"trading-atr/pkg/future"

	_ "github.com/lib/pq"
)

func main() {
    NEON_DATABASE_URL := os.Getenv("NEON_DATABASE_URL")
    if NEON_DATABASE_URL == "" {
        log.Fatal("❌ NEON_DATABASE_URL не задан")
    }

    // Подключаемся к базе
    db, err := sql.Open("postgres", NEON_DATABASE_URL)
    if err != nil {
        log.Fatal("❌ Ошибка подключения к БД:", err)
    }
    defer db.Close()

    if err := db.Ping(); err != nil {
        log.Fatal("❌ БД не отвечает:", err)
    }
    fmt.Println("✅ Подключено к Neon")

    // Создаём и загружаем фьючерс
    d := &future.Derivative{Ticker: "BRU6"}
    fmt.Println("🔄 Загружаем данные для BRU6...")
    if err := d.LoadFromMoex(); err != nil {
        log.Fatal("❌ Ошибка загрузки:", err)
    }
    fmt.Println("✅ Данные загружены!")

    // Выводим данные
    fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    fmt.Printf("📌 Тикер:           %s\n", d.Ticker)
    fmt.Printf("📌 Длинный тикер:   %s\n", d.LongTicker)
    fmt.Printf("📌 Код актива:      %s\n", d.BaseCode)
    fmt.Printf("📌 Дата экспирации: %s\n", d.Maturity.Format("02.01.2006"))
    fmt.Printf("📌 Шаг цены:        %.4f\n", d.Step)
    fmt.Printf("📌 Лот:             %.0f\n", d.LotSize)
    fmt.Printf("📌 ГО покупка:      %.2f\n", d.GoBuy)
    fmt.Printf("📌 ГО продажа:      %.2f\n", d.GoSell)
    fmt.Printf("📌 Последняя цена:  %.2f\n", d.LastPrice)
    fmt.Printf("📌 Объём (руб):     %.0f\n", d.VolumeDay)
    fmt.Printf("📌 Сделок:          %d\n", d.NumTrades)
    fmt.Printf("📌 Спред:           %.2f\n", d.Spread)
    fmt.Printf("📌 Тип:             %s\n", d.DerivativeType)
    fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

    // Проверяем статусы
    fmt.Println("\n📊 Статусы:")
    fmt.Printf("  Протух?           %t\n", d.IsExpired())
    fmt.Printf("  Торгуется?        %t\n", d.IsTrading())
    fmt.Printf("  Зомби?            %t\n", d.IsZombie())

    // Сохраняем в базу
    fmt.Println("\n💾 Сохраняем в базу...")
    if err := d.Save(db); err != nil {
        log.Fatal("❌ Ошибка сохранения:", err)
    }
    fmt.Println("✅ Данные сохранены в instrument_specs!")
}
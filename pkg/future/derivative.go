package future

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Derivative — "паспорт" дериватива (фьючерс, мини, спред, опцион).
// Это финальная структура, которая заменит старую Future.
type Derivative struct {
    // Идентификация
    Ticker     string // Короткий код: "BRU6"
    LongTicker string // Длинный код: "BR-9.26"
    BaseCode   string // Код актива: "BR"

    // Спецификация (из securities)
    Maturity       time.Time // Дата экспирации
    Step           float64   // Шаг цены
    LotSize        float64   // Размер лота
    GoBuy          float64   // Залог на покупку
    GoSell         float64   // Залог на продажу

    // Рыночные данные (из marketdata)
    LastPrice float64 // Последняя цена сделки
    OpenPrice float64 // Цена открытия дня
    HighPrice float64 // Максимум дня
    LowPrice  float64 // Минимум дня
    PrevClose float64 // Цена закрытия предыдущей сессии
    VolumeDay float64 // Объём торгов за день (руб)
    NumTrades int     // Количество сделок за день
    Spread    float64 // Спред (разница покупка/продажа)

    // Классификация
    DerivativeType string // Тип: "future", "mini", "spread", "option"
    ExchangeCode   string // Код биржи: "RFUD"
}



// LoadFromMoex заполняет все поля структуры данными из API Московской биржи.
func (d *Derivative) LoadFromMoex() error {
    url := fmt.Sprintf("https://iss.moex.com/iss/engines/futures/markets/forts/securities/%s.json", d.Ticker)

    resp, err := http.Get(url)
    if err != nil {
        return fmt.Errorf("ошибка запроса к MOEX: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("ошибка чтения ответа: %w", err)
    }

    // Временная структура для разбора JSON
    var result struct {
        Securities struct {
            Data    [][]interface{} `json:"data"`
            Columns []string        `json:"columns"`
        } `json:"securities"`
    }
    if err := json.Unmarshal(body, &result); err != nil {
        return fmt.Errorf("ошибка парсинга JSON: %w", err)
    }

    if len(result.Securities.Data) == 0 {
        return fmt.Errorf("тикер %s не найден на MOEX", d.Ticker)
    }

    // Строим карту: имя колонки -> индекс
    colIdx := make(map[string]int)
    for i, col := range result.Securities.Columns {
        colIdx[col] = i
    }

    row := result.Securities.Data[0]

    // Заполняем поля структуры
    d.Ticker = getString(row, colIdx, "SECID")
    d.LongTicker = getString(row, colIdx, "SHORTNAME")
    d.BaseCode = getString(row, colIdx, "ASSETCODE")
    d.ExchangeCode = getString(row, colIdx, "BOARDID")

    // Дата экспирации
    if dateStr := getString(row, colIdx, "LASTTRADEDATE"); dateStr != "" {
        if t, err := time.Parse("2006-01-02", dateStr); err == nil {
            d.Maturity = t
        }
    }

    // Числовые поля
    d.Step = getFloat(row, colIdx, "MINSTEP")
    d.LotSize = getFloat(row, colIdx, "LOTVOLUME")
    d.GoBuy = getFloat(row, colIdx, "INITIALMARGIN")
    d.GoSell = d.GoBuy // В API одно значение для обоих направлений

    // Тип дериватива (пока определяем по названию)
    d.DerivativeType = detectDerivativeType(d.LongTicker, d.BaseCode)

    // Загружаем рыночные данные из marketdata
var marketResult struct {
    Marketdata struct {
        Data    [][]interface{} `json:"data"`
        Columns []string        `json:"columns"`
    } `json:"marketdata"`
}
json.Unmarshal(body, &marketResult)

if len(marketResult.Marketdata.Data) > 0 {
    row := marketResult.Marketdata.Data[0]
    colIdx2 := make(map[string]int)
    for i, col := range marketResult.Marketdata.Columns {
        colIdx2[col] = i
    }
    d.LastPrice = getFloat(row, colIdx2, "LAST")
    d.OpenPrice = getFloat(row, colIdx2, "OPEN")
    d.HighPrice = getFloat(row, colIdx2, "HIGH")
    d.LowPrice = getFloat(row, colIdx2, "LOW")
    d.PrevClose = getFloat(row, colIdx2, "SETTLEPRICE")
    d.VolumeDay = getFloat(row, colIdx2, "VALTODAY")
    d.NumTrades = int(getFloat(row, colIdx2, "NUMTRADES"))
    d.Spread = getFloat(row, colIdx2, "SPREAD")
}

    return nil
}

// getString извлекает строковое значение из строки ответа API.
func getString(row []interface{}, colIdx map[string]int, colName string) string {
    if idx, ok := colIdx[colName]; ok {
        if v, ok := row[idx].(string); ok {
            return v
        }
    }
    return ""
}

// getFloat извлекает числовое значение из строки ответа API.
func getFloat(row []interface{}, colIdx map[string]int, colName string) float64 {
    if idx, ok := colIdx[colName]; ok {
        if v, ok := row[idx].(float64); ok {
            return v
        }
    }
    return 0
}

// detectDerivativeType определяет тип дериватива по косвенным признакам.
func detectDerivativeType(longTicker, baseCode string) string {
    // Если тикер длиннее 5 символов — возможно, спред
    if len(longTicker) > 8 {
        return "spread"
    }
    // Мини-фьючерсы обычно имеют короткий код из 2 букв
    if len(baseCode) == 2 && baseCode != "BR" && baseCode != "Si" {
        return "mini"
    }
    return "future"
}

// IsExpired возвращает true, если фьючерс протух (дата экспирации прошла).
func (d *Derivative) IsExpired() bool {
    if d.Maturity.IsZero() {
        return false // Если дата не задана, считаем, что не протух
    }
    return d.Maturity.Before(time.Now())
}

// IsZombie возвращает true, если фьючерс не протух, но не торгуется.
func (d *Derivative) IsZombie() bool {
    return !d.IsExpired() && !d.IsTrading()
}

// IsTrading возвращает true, если есть активные торги.
func (d *Derivative) IsTrading() bool {
    return d.LastPrice > 0 || d.VolumeDay > 0 || d.NumTrades > 0
}

// Save сохраняет данные дериватива в базу данных (таблица instrument_specs).
func (d *Derivative) Save(db *sql.DB) error {
    _, err := db.Exec(`
        INSERT INTO instrument_specs (
            ticker, step, step_price, lot_size, go_buy, go_sell,
            last_price, open_price, high_price, low_price, prev_close,
            spread, volume_day, num_trades, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW())
        ON CONFLICT (ticker) DO UPDATE SET
            step = $2, step_price = $3, lot_size = $4, go_buy = $5, go_sell = $6,
            last_price = $7, open_price = $8, high_price = $9, low_price = $10, prev_close = $11,
            spread = $12, volume_day = $13, num_trades = $14, updated_at = NOW()
    `,
        d.Ticker, d.Step, 0, d.LotSize, d.GoBuy, d.GoSell,
        d.LastPrice, d.OpenPrice, d.HighPrice, d.LowPrice, d.PrevClose,
        d.Spread, d.VolumeDay, d.NumTrades,
    )
    return err
}



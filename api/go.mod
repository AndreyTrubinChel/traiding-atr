module trading-atr/api

go 1.21

require (
	github.com/lib/pq v1.10.9
	trading-atr/pkg/future v0.0.0-00010101000000-000000000000
)

replace trading-atr/pkg/future => ../pkg/future

// ═══ binance-quant — the backtest CLI ════════════════════════════════════
// Usage: go run ./cmd/backtest [strategy]
// Strategies: buyhold (the baseline) · meanrev (the z-score mean reversion)
// The 2x futures with the taker+slippage costs and the real funding.
package main

import (
	"fmt"
	"os"

	"binance-quant/quant"
)

func main() {
	which := "meanrev"
	if len(os.Args) > 1 {
		which = os.Args[1]
	}
	candles, err := quant.LoadCandles("data/BTCUSDT-15m-futures.csv")
	if err != nil {
		fmt.Println("ERR candles:", err)
		os.Exit(1)
	}
	fd, err := quant.LoadFunding("data/BTCUSDT-funding.csv")
	if err != nil {
		fmt.Println("WARN funding:", err)
	}
	cost := quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}
	var strat quant.Strategy
	switch which {
	case "buyhold":
		strat = &quant.BuyHold{}
	case "meanrev":
		strat = &quant.MeanRev{Win: 96, Lookback: 300}
	default:
		fmt.Println("unknown strategy:", which)
		os.Exit(1)
	}
	res := quant.Run(candles, strat, cost, fd, 2.0)
	res.Print()
}

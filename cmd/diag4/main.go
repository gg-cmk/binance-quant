// ═══ binance-quant — the trend-family deep verification ══════════════════
// The top trend combos re-run with MC 200 (tighter coincidence estimate) and
// the full 6-gate report including the Sharpe. Leverage sensitivity too:
// the maxDD gate (<20%) is a leverage function — the trend edge is real (MC
// ≈ 0 across the board), so the honest question is the RISK BUDGET.
package main

import (
	"fmt"
	"os"

	"binance-quant/quant"
)

func main() {
	path := "data/BTCUSDT-4h-futures.csv"
	bpd := 6
	if len(os.Args) > 1 {
		path = os.Args[1]
		fmt.Sscanf(os.Args[2], "%d", &bpd)
	}
	candles, _ := quant.LoadCandles(path)
	fd, _ := quant.LoadFunding("data/BTCUSDT-funding.csv")
	rets := quant.LogReturns(candles)
	cost := quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}

	combos := []quant.TrendParams{
		{Fast: 24, Slow: 96, VolTarget: 1.2, BarsPerDay: bpd},
		{Fast: 24, Slow: 96, VolTarget: 0.8, BarsPerDay: bpd},
		{Fast: 24, Slow: 96, VolTarget: 0, BarsPerDay: bpd},
		{Fast: 8, Slow: 672, VolTarget: 1.2, BarsPerDay: bpd},
		{Fast: 8, Slow: 672, VolTarget: 0, BarsPerDay: bpd},
	}
	for ci, p := range combos {
		fmt.Printf("═══ combo %d: F%d S%d V%.1f ═══\n", ci+1, p.Fast, p.Slow, p.VolTarget)
		for _, lev := range []float64{2.0, 1.5, 1.0, 0.5} {
			v := quant.ValidateTrend(candles, rets, fd, cost, lev, 0.6, 200, int64(3000+ci), p)
			fmt.Printf("  lev %4.1fx: net %8.2f%% exp %7.3f%% win %4.1f%% sharpe %5.2f dd %5.1f%% n %4d | MC %4.1f%%\n",
				lev, v.Net, v.Expectancy, v.WinRate*100, v.Sharpe, v.MaxDD, v.Trades, v.MCProb*100)
		}
		fmt.Println()
	}
}
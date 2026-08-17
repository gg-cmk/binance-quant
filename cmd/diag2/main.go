// ═══ binance-quant — the net-vs-expectancy gap diagnostic ════════════════
// The best 1h combo shows exp +0.244% but net -6.04% — a contradiction.
// This decomposes the final net: closed-trade P&L vs the last open mark,
// and counts how often the MinHold override suppressed the EVT kill switch.
package main

import (
	"fmt"

	"binance-quant/quant"
)

func main() {
	candles, _ := quant.LoadCandles("data/BTCUSDT-1h-futures.csv")
	fd, _ := quant.LoadFunding("data/BTCUSDT-funding.csv")
	rets := quant.LogReturns(candles)
	p := quant.StratParams{States: 3, VolPct: 0.75, MinHold: 96, EVT: true, EVTWin: 4 * 24, EVTPct: 0.95, BarsPerDay: 24}

	d := quant.Decompose(candles, rets, fd, quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}, 2.0, 0.6, p)
	fmt.Printf("closed trades P&L (realized): %7.2f%% over %d trades\n", d.ClosedPct, d.Closed)
	fmt.Printf("final open mark-to-market   : %7.2f%% (dir %v)\n", d.OpenPct, map[int]string{0: "flat", 1: "long", -1: "short"}[int(d.OpenDir)])
	fmt.Printf("total (closed+open)         : %7.2f%%\n", d.ClosedPct+d.OpenPct)
	fmt.Printf("net (equity path)           : %7.2f%%\n", d.Net)
	fmt.Printf("maxDD (equity path)         : %7.2f%%\n", d.MaxDD)
	fmt.Printf("EVT-kill triggers           : %d (suppressed by MinHold: %d)\n", d.KillTriggers, d.KillSuppressed)
	fmt.Printf("flips (regime switches)     : %d\n", d.Flips)
	fmt.Printf("\nper-trade net P&L (%%):\n")
	for i, n := range d.TradeNets {
		fmt.Printf("  #%2d %+7.2f%%\n", i+1, n)
	}
}

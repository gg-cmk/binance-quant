// ═══ binance-quant — the stop-loss A/B + long/short split ════════════════
// The sweep showed stops did NOT help (ranked lower). Why? Hypotheses:
//   (a) chained stop-outs in a persistent downtrend pay costs repeatedly
//   (b) the signal's long side bleeds while the short side earns
// This compares the best 1h combo with and without the stop, and splits
// the P&L by direction.
package main

import (
	"fmt"

	"binance-quant/quant"
)

func run(tag string, p quant.StratParams) {
	candles, _ := quant.LoadCandles("data/BTCUSDT-1h-futures.csv")
	fd, _ := quant.LoadFunding("data/BTCUSDT-funding.csv")
	rets := quant.LogReturns(candles)
	d := quant.Decompose(candles, rets, fd, quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}, 2.0, 0.6, p)
	fmt.Printf("=== %s ===\n", tag)
	fmt.Printf("  closed P&L %7.2f%% (%d) | open %6.2f%% | net(path) %7.2f%% | maxDD %5.1f%%\n",
		d.ClosedPct, d.Closed, d.OpenPct, d.Net, d.MaxDD)
	fmt.Printf("  LONG  : %d trades  %+8.2f%%\n", d.LongN, d.LongPct)
	fmt.Printf("  SHORT : %d trades  %+8.2f%%\n", d.ShortN, d.ShortPct)
	fmt.Printf("  stop-outs: %d | EVT kills: %d (suppressed %d)\n", d.StopOuts, d.KillTriggers, d.KillSuppressed)
	fmt.Println()
}

func main() {
	base := quant.StratParams{States: 3, VolPct: 0.75, MinHold: 96, EVT: false, EVTWin: 4 * 24, EVTPct: 0.95, BarsPerDay: 24}
	run("no stop  (X 0.000)", base)
	p2 := base
	p2.StopPct = 0.05
	run("stop 5%  (X 0.050)", p2)
	p3 := base
	p3.StopPct = 0.025
	run("stop 2.5% (X 0.025)", p3)
	p4 := base
	p4.StopPct = 0.10
	run("stop 10% (X 0.100)", p4)
}
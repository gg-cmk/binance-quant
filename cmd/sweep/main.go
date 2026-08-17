// ═══ binance-quant — the honest parameter sweep ══════════════════════════
// The grid × the walk-forward × the Monte Carlo — the BEST combo must
// survive the multiple-comparison: the Bonferroni-corrected coincidence
// (MCProb × trials < 1%) — otherwise every winner is a data-snooping
// artifact and the family is discarded.
package main

import (
	"fmt"
	"os"
	"sort"

	"binance-quant/quant"
)

type combo struct {
	p    quant.StratParams
	net  float64
	exp  float64
	wr   float64
	dd   float64
	trades int
	mcp  float64
}

func main() {
	csvPath := "data/BTCUSDT-15m-futures.csv"
	bpd := 96 // the bars per day (96=15m, 24=1h, 6=4h)
	if len(os.Args) > 1 {
		csvPath = os.Args[1]
	}
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &bpd)
	}
	candles, err := quant.LoadCandles(csvPath)
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	fd, _ := quant.LoadFunding("data/BTCUSDT-funding.csv")
	cost := quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}
	rets := quant.LogReturns(candles)

	trials := 0
	results := []combo{}
	for _, states := range []int{2, 3} {
		for _, vol := range []float64{0.75, 0.85, 0.95} {
			for _, hold := range []int{4, 16, 96} {
				for _, evt := range []bool{true, false} {
					for _, stop := range []float64{0, 0.025, 0.05} {
						p := quant.StratParams{States: states, VolPct: vol, MinHold: hold, EVT: evt, EVTWin: 4 * bpd, EVTPct: 0.95, BarsPerDay: bpd, StopPct: stop}
						v := quant.Validate(candles, rets, fd, cost, 2.0, 0.6, 60, int64(1000+trials), p)
						trials++
						results = append(results, combo{p: p, net: v.Net, exp: v.Expectancy, wr: v.WinRate, dd: v.MaxDD, trades: v.Trades, mcp: v.MCProb})
						fmt.Printf("S%d V%.2f H%d E%v X%.3f: net %7.2f%% exp %6.3f%% win %4.1f%% dd %5.1f%% n %4d MC %4.1f%%\n",
							states, vol, hold, evt, stop, v.Net, v.Expectancy, v.WinRate*100, v.MaxDD, v.Trades, v.MCProb*100)
					}
				}
			}
		}
	}
	sort.Slice(results, func(a, b int) bool { return results[a].net > results[b].net })
	fmt.Printf("\n═══ BEST 5 (of %d trials) ═══\n", trials)
	for i, c := range results[:min(5, len(results))] {
		bonf := c.mcp * float64(trials)
		pass := c.net > 0 && bonf < 0.01
		fmt.Printf("#%d S%d V%.2f H%d E%v X%.3f: net %7.2f%% exp %6.3f%% win %4.1f%% dd %5.1f%% n %4d | MC %.1f%% × %d = %.1f%% %v\n",
			i+1, c.p.States, c.p.VolPct, c.p.MinHold, c.p.EVT, c.p.StopPct, c.net, c.exp, c.wr*100, c.dd, c.trades, c.mcp*100, trials, bonf*100,
			map[bool]string{true: "★ PASS", false: "✗ fail"}[pass])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

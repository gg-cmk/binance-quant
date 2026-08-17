// ═══ binance-quant — family 2 sweep: the vol-targeted trend follower ══════
// The HMM family is dead (docs/family-1-hmm-har-evt-DEAD.md). The trend
// follower follows the EMA-cross direction (adaptive by construction) and
// stands aside under high vol (the HAR-style realized vol targeting).
// Same honest gates: walk-forward OOS + Monte Carlo + Bonferroni. A fresh
// family = a fresh multiple-comparison (the family is small on purpose).
package main

import (
	"fmt"
	"os"
	"sort"

	"binance-quant/quant"
)

type tcombo struct {
	p      quant.TrendParams
	net    float64
	exp    float64
	wr     float64
	dd     float64
	trades int
	mcp    float64
}

func main() {
	csvPath := "data/BTCUSDT-1h-futures.csv"
	bpd := 24
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
	results := []tcombo{}
	for _, fast := range []int{8, 24} {
		for _, slow := range []int{96, 288, 672} {
			for _, vt := range []float64{0, 0.8, 1.2} {
				p := quant.TrendParams{Fast: fast, Slow: slow, VolTarget: vt, BarsPerDay: bpd}
				v := quant.ValidateTrend(candles, rets, fd, cost, 2.0, 0.6, 60, int64(2000+trials), p)
				trials++
				results = append(results, tcombo{p: p, net: v.Net, exp: v.Expectancy, wr: v.WinRate, dd: v.MaxDD, trades: v.Trades, mcp: v.MCProb})
				fmt.Printf("F%d S%d V%4.1f: net %8.2f%% exp %7.3f%% win %4.1f%% dd %5.1f%% n %4d MC %4.1f%%\n",
					fast, slow, vt, v.Net, v.Expectancy, v.WinRate*100, v.MaxDD, v.Trades, v.MCProb*100)
			}
		}
	}
	sort.Slice(results, func(a, b int) bool { return results[a].net > results[b].net })
	fmt.Printf("\n═══ BEST 5 (of %d trials) ═══\n", trials)
	for i, c := range results[:min(5, len(results))] {
		bonf := c.mcp * float64(trials)
		pass := c.net > 0 && bonf < 0.01
		winGate := c.wr >= 0.51
		fmt.Printf("#%d F%d S%d V%4.1f: net %8.2f%% exp %7.3f%% win %4.1f%%(>=51%%: %v) dd %5.1f%%(<20%%: %v) n %4d | MC %.1f%% × %d = %.1f%% %v\n",
			i+1, c.p.Fast, c.p.Slow, c.p.VolTarget, c.net, c.exp, c.wr*100, winGate, c.dd, c.dd < 20, c.trades, c.mcp*100, trials, bonf*100,
			map[bool]string{true: "★ PASS", false: "✗ fail"}[pass])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
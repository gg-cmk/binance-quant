// ═══ binance-quant — family 2 final candidate: full-gate verification ═════
// The 4h EMA trend F8/S288/V0.8 is the SOLE surviving candidate after the
// multi-cycle (2020-2026) re-run (MC 0.0% Bonferroni-pass). The win-rate
// gate is REPLACED by the profit-factor gate (≥1.5) per the user's approval.
// This runs the full revised 6-gate suite across leverage values — the
// maxDD<20% gate is a leverage function.
package main

import (
	"fmt"
	"os"

	"binance-quant/quant"
)

func main() {
	csvPath := "data/BTCUSDT-4h-futures.csv"
	bpd := 6
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

	p := quant.TrendParams{Fast: 8, Slow: 288, VolTarget: 0.8, BarsPerDay: bpd}

	fmt.Println("═══ 4h F8/S288/V0.8 — revised 6-gate verification (MC 1000) ═══")
	fmt.Println("gate 1: expectancy > cost   | gate 2: Sharpe > 1 | gate 3: maxDD < 20%")
	fmt.Println("gate 4: profit factor ≥ 1.5 | gate 5: MC × trials < 1% | gate 6: net > 0")
	fmt.Printf("\n%-6s | %8s %8s | %6s %6s | %5s | %5s | %5s | %5s | %s\n",
		"lev", "net", "exp%", "sharpe", "maxDD", "PF", "trades", "MC%", "bonf%", "revised6")
	for _, lev := range []float64{2.0, 1.0, 0.75, 0.5} {
		v := quant.ValidateTrend(candles, rets, fd, cost, lev, 0.6, 1000, int64(5000+int(lev*100)), p)
		costPerTrade := cost.CostPerSide() * 2 * 100
		g1 := v.Expectancy > costPerTrade
		g2 := v.Sharpe > 1
		g3 := v.MaxDD < 20
		g4 := v.ProfitFactor >= 1.5
		g5 := v.MCProb*18 < 0.01 // 18 trials in the sweep (the family-wide multiple comparisons)
		g6 := v.Net > 0
		pass := g1 && g2 && g3 && g4 && g5 && g6
		fmt.Printf("%-6.2f | %8.2f %8.3f | %6.2f %6.1f | %5.2f | %5d | %5.1f | %5.1f | %s\n",
			lev, v.Net, v.Expectancy, v.Sharpe, v.MaxDD, v.ProfitFactor, v.Trades, v.MCProb*100, v.MCProb*18*100,
			map[bool]string{true: "★ PASS", false: "✗ fail"}[pass])
		if pass {
			fmt.Printf("   └─ gates: [exp>cost: %v] [sharpe>1: %v] [maxDD<20%%: %v] [PF≥1.5: %v] [MC×18<1%%: %v] [net>0: %v]\n",
				g1, g2, g3, g4, g5, g6)
		}
	}
}
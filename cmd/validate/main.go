// ═══ binance-quant — the validation CLI ══════════════════════════════════
// The walk-forward + Monte Carlo validation of the HMM-regime × HAR-vol
// strategy on the 2x futures. The honest gate report.
package main

import (
	"fmt"
	"os"

	"binance-quant/quant"
)

func main() {
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
	rets := quant.LogReturns(candles)

	v := quant.Validate(candles, rets, fd, cost, 2.0, 0.6, 200, 42)
	fmt.Printf("═══ WALK-FORWARD (train 60%% / test 40%%) ═══\n")
	fmt.Printf("trades    %d\n", v.Trades)
	fmt.Printf("win rate  %.1f%%\n", v.WinRate*100)
	fmt.Printf("expectancy %.4f%% / trade\n", v.Expectancy)
	fmt.Printf("net       %.2f%%\n", v.Net)
	fmt.Printf("max DD    %.1f%%\n", v.MaxDD)
	fmt.Printf("MC better %d / 200 (coincidence %.1f%%)\n", v.MCBetter, v.MCProb*100)
	g := quant.CheckGates(v, cost.CostPerSide())
	fmt.Printf("GATES     %v\n", map[bool]string{true: "PASS", false: "FAIL"}[g.Pass])
	for _, f := range g.Fail {
		fmt.Println("  ✗", f)
	}
}

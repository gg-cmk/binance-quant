// ═══ binance-quant — family 3 sweep: the EWS×EVT risk overlay ═════════════
// buyhold-2x + critical-slowing-down overlay. The honest success criterion:
// RISK IMPROVEMENT vs the buyhold-2x base (net ↑, maxDD ↓, Sharpe ↑) with
// the MC proving the overlay timing is not luck. Same discipline as the
// dead families: walk-forward OOS + Monte Carlo + Bonferroni.
package main

import (
	"fmt"
	"os"

	"binance-quant/quant"
)

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
	fmt.Printf("%-34s | %8s %8s %8s | %6s %6s | %4s %5s %6s %5s %6s | %s\n",
		"combo", "net", "buyNet", "impr", "dd", "buyDD", "n", "win", "exp", "flat", "MC", "verdict")
	for _, win := range []int{96, 288} {
		for _, ac := range []float64{0.90, 0.95} {
			for _, vr := range []float64{0.90} {
				for _, evt := range []bool{true, false} {
					p := quant.OverlayParams{EwsWin: win, AcPct: ac, VarPct: vr, EvtOn: evt, EvtWin: 4 * bpd, EvtPct: 0.95, BarsPerDay: bpd}
					o := quant.ValidateOverlay(candles, rets, fd, cost, 2.0, 0.6, 200, int64(4000+trials), p)
					trials++
					// the honest gates for a RISK OVERLAY:
					//  1. net improvement vs buyhold > 0
					//  2. maxDD < 20% (the risk gate)
					//  3. Sharpe > 1
					//  4. MC coincidence < 1% × trials (Bonferroni)
					bonf := o.MCProb * float64(trials)
					improved := o.Net > o.BuyNet
					ddOK := o.MaxDD < 20
					shOK := o.Sharpe > 1
					mcOK := bonf < 0.01
					verdict := "✗"
					if improved && ddOK && shOK && mcOK {
						verdict = "★ PASS"
					}
					fmt.Printf("W%-4d A%.2f V%.2f E%v   | %8.2f %8.2f %8.2f | %6.1f %6.1f | %4d %4.1f%% %6.3f %5.0f%% %5.1f%% | %s\n",
						win, ac, vr, evt, o.Net, o.BuyNet, o.NetImprove, o.MaxDD, o.BuyMaxDD, o.Trades, o.WinRate*100, o.Exp, o.FlatFrac*100, o.MCProb*100, verdict)
				}
			}
		}
	}
	fmt.Printf("\ntrials: %d | Bonferroni multiplier: ×%d (<1%% required)\n", trials, trials)
}
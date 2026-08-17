// ═══ binance-quant — the zero-cost diagnostic ═════════════════════════════
// Runs the best sweep combo with cost = 0 to isolate the RAW signal edge
// from the transaction-cost drag. Also prints the cost hurdle itself.
package main

import (
	"fmt"

	"binance-quant/quant"
)

func main() {
	candles, err := quant.LoadCandles("data/BTCUSDT-15m-futures.csv")
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	fd, _ := quant.LoadFunding("data/BTCUSDT-funding.csv")
	rets := quant.LogReturns(candles)
	p := quant.StratParams{States: 2, VolPct: 0.75, MinHold: 16, EVT: true, EVTWin: 4 * 96, EVTPct: 0.95}

	// the real cost model (what the sweep used)
	real := quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}
	fmt.Printf("cost per side: %.4f%%  round-trip: %.4f%%  (taker 0.05%% + slip 8bp)\n",
		real.CostPerSide()*100, real.CostPerSide()*2*100)

	v := quant.Validate(candles, rets, fd, real, 2.0, 0.6, 60, 1000, p)
	fmt.Printf("WITH costs : net %7.2f%% exp %6.3f%% win %4.1f%% dd %5.1f%% n %4d MC %4.1f%%\n",
		v.Net, v.Expectancy, v.WinRate*100, v.MaxDD, v.Trades, v.MCProb*100)

	// the same pipeline with zero taker/slippage — funding still charged
	free := quant.CostModel{TakerPct: 0, SlippagePct: 0}
	v0 := quant.Validate(candles, rets, fd, free, 2.0, 0.6, 60, 1001, p)
	fmt.Printf("NO taker/slip: net %7.2f%% exp %6.3f%% win %4.1f%% dd %5.1f%% n %4d MC %4.1f%%\n",
		v0.Net, v0.Expectancy, v0.WinRate*100, v0.MaxDD, v0.Trades, v0.MCProb*100)

	// funding only — the pure cost of holding
	vf := quant.Validate(candles, rets, quant.Funding{}, free, 2.0, 0.6, 60, 1002, p)
	fmt.Printf("ZERO costs  : net %7.2f%% exp %6.3f%% win %4.1f%% dd %5.1f%% n %4d MC %4.1f%%\n",
		vf.Net, vf.Expectancy, vf.WinRate*100, vf.MaxDD, vf.Trades, vf.MCProb*100)
}

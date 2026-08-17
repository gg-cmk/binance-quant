// ═══ binance-quant — the overlay +Inf debug (fixed entry) ═════════════════
package main

import (
	"fmt"

	"binance-quant/quant"
)

func main() {
	candles, _ := quant.LoadCandles("data/BTCUSDT-1h-futures.csv")
	fd, _ := quant.LoadFunding("data/BTCUSDT-funding.csv")
	cost := quant.CostModel{TakerPct: 0.0005, SlippagePct: 0.0008}
	rets := quant.LogReturns(candles)
	p := quant.OverlayParams{EwsWin: 96, AcPct: 0.90, VarPct: 0.90, EvtOn: false, EvtWin: 96, EvtPct: 0.95, BarsPerDay: 24}

	o := quant.ValidateOverlay(candles, rets, fd, cost, 2.0, 0.6, 200, 4000, p)
	fmt.Printf("net=%.4f buyNet=%.4f impr=%.4f dd=%.2f buyDD=%.2f trades=%d win=%.1f%% exp=%.4f flat=%.2f%% MC=%d (%.1f%%)\n",
		o.Net, o.BuyNet, o.NetImprove, o.MaxDD, o.BuyMaxDD, o.Trades, o.WinRate*100, o.Exp, o.FlatFrac*100, o.MCBetter, o.MCProb*100)
}
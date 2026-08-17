// ═══ binance-quant — the baseline strategies ═════════════════════════════
package quant


// BuyHold — the sanity baseline: always long at 2x (the market itself).
type BuyHold struct{}

func (b *BuyHold) Signal(i int, candles []Candle, ctx *StratCtx) Position {
	return Long
}
func (b *BuyHold) Name() string { return "buyhold-2x" }

// MeanRev — the z-score mean reversion (Bollinger-like):
//   z = (close − SMA) / σ of the window
//   long when z ≤ −2 (oversold), flat when z ≥ 0
// The 2x sizing is applied by the engine. The honest expectation for a
// direction strategy on 15m BTC is a ~50-55% hit rate — the gate decides.
type MeanRev struct {
	Win      int // the SMA/σ window in bars (96 = a day of 15m)
	Lookback int // the minimum history before trading (warm-up)
}

func (m *MeanRev) Signal(i int, candles []Candle, ctx *StratCtx) Position {
	if i < m.Lookback || i < m.Win+1 {
		return Flat
	}
	sum := 0.0
	for j := i - m.Win; j < i; j++ {
		sum += candles[j].Close
	}
	mean := sum / float64(m.Win)
	v := 0.0
	for j := i - m.Win; j < i; j++ {
		d := candles[j].Close - mean
		v += d * d
	}
	sd := sqrt(v / float64(m.Win))
	if sd == 0 {
		return Flat
	}
	z := (candles[i].Close - mean) / sd
	if z <= -2.0 {
		return Long
	}
	if z >= 0 {
		return Flat
	}
	return Flat
}
func (m *MeanRev) Name() string { return "meanrev-2x" }

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for k := 0; k < 20; k++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

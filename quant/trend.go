// ═══ binance-quant — family 2: the vol-targeted trend follower ════════════
// The HMM×HAR×EVT direction family is DEAD (docs/family-1-hmm-har-evt-DEAD.md).
// The post-mortem: HMM regime means do NOT generalize OOS (train +0.175%/day
// long state → OOS long -27.4%); the short side's edge was a single bear
// period (period-sniping, not signal). Direction should not be PREDICTED —
// it should be FOLLOWED. The trend follower:
//
//   direction = EMA(fast) vs EMA(slow)   (the trend, adaptive by nature)
//   size      = min(1, targetVol / HAR-RV forecast)   (vol targeting)
//   final     = direction × size × 2x max   (the engine's leverage fixed 2x)
//
// The trend follows whatever regime is ACTUALLY happening, so a regime drift
// cannot invalidate it. The vol targeting keeps the size inverse to the
// forecast risk — the edge the research (deleg_c6e2399b) identified as robust.
package quant

import (
	"math"
	"math/rand"
)

// TrendParams — the trend-follower knobs.
type TrendParams struct {
	Fast      int     // the fast EMA window (bars)
	Slow      int     // the slow EMA window (bars) — the direction line
	VolTarget float64 // the target daily vol (0 = no vol targeting, fixed 2x)
	BarsPerDay int    // the bars per day of the timeframe
}

// TrendFollow — the strategy in the Strategy interface (for the engine).
type TrendFollow struct {
	P TrendParams
}

func (t *TrendFollow) Name() string { return "trend-follow" }

// Signal — the EMA-cross direction × the vol-targeted size, expressed as a
// Position. The engine's leverage stays at 2x; the size scaling happens by
// choosing Flat when the vol target says the risk is too high (a binary
// approximation of the continuous sizing: we never exceed 2x, we only
// stand aside when the forecast vol exceeds the target).
// The signal at index i uses ONLY the closes up to i-1 (no look-ahead) —
// the fill happens at the open of bar i.
func (t *TrendFollow) Signal(i int, candles []Candle, ctx *StratCtx) Position {
	if i <= t.P.Slow {
		return Flat
	}
	// the EMAs, computed up to i-1
	fast := ema(candles, i-1, t.P.Fast)
	slow := ema(candles, i-1, t.P.Slow)
	dir := Flat
	if fast > slow {
		dir = Long
	} else if fast < slow {
		dir = Short
	}
	// the vol targeting: the realized vol of the last BarsPerDay bars
	// (up to i-1), annualized — stand aside when it is above the target
	if t.P.VolTarget > 0 && t.P.BarsPerDay > 0 {
		rv := realizedVol(candles, i-1, t.P.BarsPerDay, t.P.BarsPerDay)
		if rv > t.P.VolTarget {
			return Flat
		}
	}
	return dir
}

// ema — the exponential moving average at index i (the standard 2/(n+1) decay).
func ema(candles []Candle, i, n int) float64 {
	if i < n {
		return 0
	}
	alpha := 2.0 / float64(n+1)
	e := candles[i-n+1].Close
	for k := i - n + 2; k <= i; k++ {
		e = alpha*candles[k].Close + (1-alpha)*e
	}
	return e
}

// realizedVol — the rolling realized volatility of the close-to-close log
// returns over the last n bars (ending at index i), annualized.
func realizedVol(candles []Candle, i, n, barsPerDay int) float64 {
	if i < n+1 {
		return 0
	}
	sum := 0.0
	for k := i - n + 1; k <= i; k++ {
		r := math.Log(candles[k].Close / candles[k-1].Close)
		sum += r * r
	}
	// the per-bar variance × bars/day × 365 → the annualized vol
	return math.Sqrt(sum/float64(n)) * math.Sqrt(float64(barsPerDay)*365)
}

// ValidateTrend — the walk-forward + Monte Carlo + the 6 gates for the trend
// follower. The train is NOT used to fit the EMA windows (the trend is
// adaptive by construction) — the train/test split is still enforced so the
// OOS numbers are honest, and the Monte Carlo shuffles the OOS returns so the
// null distribution has the same costs and the same trade timing structure.
func ValidateTrend(candles []Candle, returns []float64, fd Funding, cost CostModel, leverage, trainFrac float64, mcN int, seed int64, p TrendParams) Validation {
	v := Validation{TrainFrac: trainFrac}
	split := int(float64(len(candles)) * trainFrac)
	testC := candles[split:]
	testR := returns[split-1:]

	// the trade loop (the test only) — same accounting as Validate
	pos := Flat
	entry, entryT := 0.0, int64(0)
	equity := 1.0
	var trades []Trade
	eq := make([]float64, len(testC))
	eq[0] = 1.0
	for i := 1; i < len(testC); i++ {
		gi := split + i // the global candle index
		g := split + i  // the global index into the FULL candles for the EMAs
		_ = gi
		strat := &TrendFollow{P: p}
		sig := strat.Signal(g, candles, nil)

		// the vol-target size: Flat handled inside Signal (binary approx)
		if sig != pos {
			if pos != Flat {
				exit := testC[i].Open
				raw := (exit - entry) / entry
				if pos == Short {
					raw = -raw
				}
				fund := fd.fundingFor(entryT, testC[i].Time)
				costPct := cost.CostPerSide()*2 + fund
				equity *= 1 + raw*leverage - costPct
				trades = append(trades, Trade{OpenI: i - 1, CloseI: i, Dir: pos, Entry: entry, Exit: exit, RetPct: raw * 100, CostPct: costPct * 100, FundingPct: fund * 100})
			}
			if sig != Flat {
				entry, entryT = testC[i].Open, testC[i].Time
			}
			pos = sig
		}
		if pos != Flat {
			raw := (testC[i].Close - entry) / entry
			if pos == Short {
				raw = -raw
			}
			eq[i] = equity * (1 + raw*leverage)
		} else {
			eq[i] = equity
		}
	}
	// the metrics
	v.Trades = len(trades)
	if len(trades) > 0 {
		wins, sum := 0, 0.0
		for _, t := range trades {
			net := t.RetPct*leverage - t.CostPct
			sum += net
			if net > 0 {
				wins++
			}
		}
		v.WinRate = float64(wins) / float64(len(trades))
		v.Expectancy = sum / float64(len(trades))
	}
	v.Net = (eq[len(eq)-1] - 1) * 100
	// the Sharpe of the per-bar equity returns (annualized)
	{
		er := make([]float64, 0, len(eq))
		for i := 1; i < len(eq); i++ {
			er = append(er, eq[i]/eq[i-1]-1)
		}
		mean, sd := 0.0, 0.0
		if len(er) > 1 {
			for _, r := range er {
				mean += r
			}
			mean /= float64(len(er))
			for _, r := range er {
				sd += (r - mean) * (r - mean)
			}
			sd = math.Sqrt(sd / float64(len(er)-1))
		}
		if sd > 0 {
			bpy := float64(p.BarsPerDay) * 365
			v.Sharpe = mean / sd * math.Sqrt(bpy)
		}
	}
	peak := eq[0]
	for _, e := range eq {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := (peak - e) / peak * 100
			if dd > v.MaxDD {
				v.MaxDD = dd
			}
		}
	}
	// the Monte Carlo: shuffled OOS returns → the same course → the null
	rng := rand.New(rand.NewSource(seed))
	shuffled := make([]float64, len(testR))
	better := 0
	for k := 0; k < mcN; k++ {
		copy(shuffled, testR)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		// the synthetic candles from the shuffled returns (the same closing
		// path, the EMAs re-run on the shuffled close series)
		syn := make([]Candle, len(testC))
		copy(syn, testC)
		pc := testC[0].Close
		for i := 1; i < len(syn); i++ {
			pc = pc * math.Exp(shuffled[i-1])
			syn[i].Close = pc
			syn[i].Open = syn[i-1].Close
			syn[i].High = syn[i].Close * 1.001
			syn[i].Low = syn[i].Close * 0.999
		}
		eq2 := 1.0
		pos2 := Flat
		en2, enT2 := 0.0, int64(0)
		for i := 1; i < len(syn); i++ {
			strat2 := &TrendFollow{P: p}
			sig2 := strat2.Signal(i, syn, nil)
			if sig2 != pos2 {
				if pos2 != Flat {
					exit := syn[i].Open
					raw := (exit - en2) / en2
					if pos2 == Short {
						raw = -raw
					}
					fund := fd.fundingFor(enT2, syn[i].Time)
					costPct := cost.CostPerSide()*2 + fund
					eq2 *= 1 + raw*leverage - costPct
				}
				if sig2 != Flat {
					en2, enT2 = syn[i].Open, syn[i].Time
				}
				pos2 = sig2
			}
		}
		net2 := (eq2 - 1) * 100
		if net2 >= v.Net {
			better++
		}
	}
	v.MCBetter = better
	v.MCProb = float64(better) / float64(mcN)
	return v
}
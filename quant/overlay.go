// ═══ binance-quant — family 3: the EWS×EVT risk overlay on buyhold-2x ════
// The pure-science research (docs research 2026-08-17, 98 verified papers)
// concluded: individual-event prediction is impossible; the transferable
// edge is STATISTICAL and STRUCTURAL — return periods (EVT) and critical
// slowing-down early-warning signals (EWS). This family applies BOTH as a
// RISK-MANAGEMENT OVERLAY on the fixed 2x long base:
//
//   base  : always Long at 2x (the buyhold-2x sanity benchmark)
//   EWS   : rolling lag-1 autocorrelation + rolling variance — when BOTH
//           rise above their train percentiles (the critical-slowing
//           signature, Scheffer 2009 / Dakos 2008), the exposure → Flat
//   EVT   : rolling GPD Expected-Shortfall(0.99) > train killThr → Flat
//
// The honest success criterion is NOT direction skill — it is RISK
// IMPROVEMENT vs the buyhold-2x base: net ↑, maxDD ↓, Sharpe ↑, with the
// Monte Carlo proving the overlay timing is not luck.
package quant

import (
	"math"
	"math/rand"
	"sort"
)

// OverlayParams — the EWS×EVT overlay knobs.
type OverlayParams struct {
	EwsWin   int     // the AC1/variance rolling window (bars)
	AcPct    float64 // the train AC1 percentile trigger (0.90 = 90th)
	VarPct   float64 // the train variance percentile trigger (0.90)
	EvtOn    bool    // the EVT-POT kill switch on/off
	EvtWin   int     // the rolling window for the EVT fit (bars)
	EvtPct   float64 // the train ES percentile kill threshold
	BarsPerDay int   // the bars per day of the timeframe
}

// lagOneAC — the lag-1 autocorrelation of the returns over the window
// ending at index i (inclusive): corr(r[t], r[t-1]).
func lagOneAC(returns []float64, i, win int) float64 {
	if i < win+1 {
		return 0
	}
	var s, sx, sy, sxx, syy float64
	for t := i - win + 1; t <= i; t++ {
		x := returns[t-1]
		y := returns[t]
		s += x * y
		sx += x
		sy += y
		sxx += x * x
		syy += y * y
	}
	n := float64(win)
	den := math.Sqrt((n*sxx - sx*sx) * (n*syy - sy*sy))
	if den <= 0 {
		return 0
	}
	return (n*s - sx*sy) / den
}

// ewssig — the EWS trigger at bar i: true when BOTH the AC1 and the
// variance exceed their train thresholds (the critical-slowing signature).
func (p OverlayParams) ewssig(ac1, varMeas float64, acThr, varThr float64) bool {
	return ac1 > acThr && varMeas > varThr
}

// pct — the q-th percentile of a slice.
func pct(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64{}, v...)
	sort.Float64s(s)
	return s[int(q*float64(len(s)-1))]
}

// OverlayResult — the overlay validation output.
type OverlayResult struct {
	Net          float64 // the overlay net %
	BuyNet       float64 // the buyhold-2x net % over the same window
	NetImprove   float64 // Net − BuyNet
	MaxDD        float64
	BuyMaxDD     float64
	Sharpe       float64
	BuySharpe    float64
	Trades       int     // the closed Long positions (risk-off round trips)
	WinRate      float64
	Exp          float64 // the mean net % per closed Long trade
	MCBetter     int
	MCProb       float64 // P(shuffled improvement ≥ real improvement)
	EwsFires     int     // the number of risk-off episodes entered
	EvtFires     int
	FlatFrac     float64 // the fraction of test bars spent flat (risk-off)
}

// RollEWS — the rolling AC1 + variance series over the returns.
func RollEWS(returns []float64, win int) (ac1 []float64, vari []float64) {
	ac1 = make([]float64, len(returns))
	vari = make([]float64, len(returns))
	for i := win; i < len(returns); i++ {
		ac1[i] = lagOneAC(returns, i, win)
		w := returns[i-win+1 : i+1]
		m := 0.0
		for _, r := range w {
			m += r
		}
		m /= float64(len(w))
		v := 0.0
		for _, r := range w {
			v += (r - m) * (r - m)
		}
		vari[i] = v / float64(len(w))
	}
	return
}

// ValidateOverlay — the walk-forward + Monte Carlo for the EWS×EVT overlay.
// The train sets the EWS/EVT thresholds; the test runs the overlay on the
// OOS window; the Monte Carlo shuffles the OOS returns and measures how
// often a shuffled path with the SAME overlay achieves an equal or better
// improvement vs its own buyhold — the coincidence probability.
func ValidateOverlay(candles []Candle, returns []float64, fd Funding, cost CostModel, leverage, trainFrac float64, mcN int, seed int64, p OverlayParams) OverlayResult {
	o := OverlayResult{}
	split := int(float64(len(candles)) * trainFrac)
	testC := candles[split:]
	testR := returns[split-1:]
	n := len(returns)

	// the EWS series + the train thresholds
	ac1, vari := RollEWS(returns, p.EwsWin)
	acThr := pct(ac1[split/2:split], p.AcPct)   // from the train only
	varThr := pct(vari[split/2:split], p.VarPct)

	// the EVT kill threshold from the train
	killThr := 0.0
	if p.EvtOn {
		esList := []float64{}
		evtStep := p.BarsPerDay
		for i := 4 * p.BarsPerDay; i < split; i += evtStep {
			win := returns[max(0, i-p.EvtWin):i]
			losses := make([]float64, len(win))
			for k, r := range win {
				losses[k] = -r
			}
			u := percentile(losses, 0.95)
			f := FitPOT(losses, u)
			if f.Excess > 20 {
				esList = append(esList, f.ExpectedShortfall(len(win), 0.99))
			}
		}
		if len(esList) > 0 {
			killThr = percentile(esList, p.EvtPct)
		}
	}

	// the test loop: the base is ALWAYS Long — the overlay goes Flat on EWS/EVT
	pos := Long
	entry, entryT := testC[0].Open, testC[0].Time // the base is long from bar 0
	equity := 1.0
	flatBars := 0
	var trades []Trade
	eq := make([]float64, len(testC))
	eq[0] = 1.0
	for i := 1; i < len(testC); i++ {
		gi := split - 1 + i // the global index into the returns/EWS series
		sig := Long
		if gi < n && ac1[gi] > acThr && vari[gi] > varThr {
			sig = Flat
			o.EwsFires++
		}
		if p.EvtOn && killThr > 0 && gi >= p.EvtWin {
			win := returns[max(0, gi-p.EvtWin):gi]
			losses := make([]float64, len(win))
			for k, r := range win {
				losses[k] = -r
			}
			u := percentile(losses, 0.95)
			f := FitPOT(losses, u)
			if f.Excess > 20 && f.ExpectedShortfall(len(win), 0.99) > killThr {
				sig = Flat
				o.EvtFires++
			}
		}
		// the equity accounting: the Mark is on the CURRENT position only
		if sig != pos {
			if pos != Flat {
				exit := testC[i].Open
				raw := (exit - entry) / entry
				fund := fd.fundingFor(entryT, testC[i].Time)
				costPct := cost.CostPerSide()*2 + fund
				equity *= 1 + raw*leverage - costPct
				trades = append(trades, Trade{OpenI: i - 1, CloseI: i, Dir: Long, Entry: entry, Exit: exit, RetPct: raw * 100, CostPct: costPct * 100, FundingPct: fund * 100})
			}
			if sig != Flat {
				entry, entryT = testC[i].Open, testC[i].Time
			}
			pos = sig
		}
		if pos != Flat {
			raw := (testC[i].Close - entry) / entry
			eq[i] = equity * (1 + raw*leverage)
		} else {
			eq[i] = equity
			flatBars++
		}
	}
	o.FlatFrac = float64(flatBars) / float64(len(testC))

	// the metrics
	o.Net = (eq[len(eq)-1] - 1) * 100
	// the buyhold benchmark over the SAME window (always Long, no exits)
	buyEq := make([]float64, len(testC))
	buyEq[0] = 1.0
	bEntry := testC[0].Open
	bEquity := 1.0
	for i := 1; i < len(testC); i++ {
		bendar := (testC[i].Close - bEntry) / bEntry
		buyEq[i] = bEquity * (1 + bendar*leverage)
		if i == len(testC)-1 {
			fund := fd.fundingFor(testC[0].Time, testC[i].Time)
			costPct := cost.CostPerSide()*2 + fund
			bEquity *= 1 + bendar*leverage - costPct
		}
	}
	o.BuyNet = (buyEq[len(buyEq)-1] - 1) * 100
	o.NetImprove = o.Net - o.BuyNet

	// the Sharpe of the overlay equity
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
		o.Sharpe = mean / sd * math.Sqrt(float64(p.BarsPerDay)*365)
	}
	// the buyhold Sharpe
	ber := make([]float64, 0, len(buyEq))
	for i := 1; i < len(buyEq); i++ {
		ber = append(ber, buyEq[i]/buyEq[i-1]-1)
	}
	mean, sd = 0.0, 0.0
	if len(ber) > 1 {
		for _, r := range ber {
			mean += r
		}
		mean /= float64(len(ber))
		for _, r := range ber {
			sd += (r - mean) * (r - mean)
		}
		sd = math.Sqrt(sd / float64(len(ber)-1))
	}
	if sd > 0 {
		o.BuySharpe = mean / sd * math.Sqrt(float64(p.BarsPerDay)*365)
	}

	// the max drawdowns
	peak := eq[0]
	for _, e := range eq {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := (peak - e) / peak * 100
			if dd > o.MaxDD {
				o.MaxDD = dd
			}
		}
	}
	bpeak := buyEq[0]
	for _, e := range buyEq {
		if e > bpeak {
			bpeak = e
		}
		if bpeak > 0 {
			dd := (bpeak - e) / bpeak * 100
			if dd > o.BuyMaxDD {
				o.BuyMaxDD = dd
			}
		}
	}

	// the trade stats
	o.Trades = len(trades)
	if len(trades) > 0 {
		wins, sum := 0, 0.0
		for _, t := range trades {
			net := t.RetPct*leverage - t.CostPct
			sum += net
			if net > 0 {
				wins++
			}
		}
		o.WinRate = float64(wins) / float64(len(trades))
		o.Exp = sum / float64(len(trades))
	}

	// the Monte Carlo: shuffle the OOS returns → synthetic price path → the
	// SAME overlay → the coincidence probability of the NET IMPROVEMENT
	rng := rand.New(rand.NewSource(seed))
	shuffled := make([]float64, len(testR))
	better := 0
	for k := 0; k < mcN; k++ {
		copy(shuffled, testR)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
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
		sR := make([]float64, 0)
		_ = sR
		// the EWS series on the SYNTHETIC path — the overlay thresholds:
		// recompute from the synthetic train (same procedure, no cheat)
		sync := append(append([]Candle{}, candles[:split]...), syn...)
		synRets := LogReturns(sync)
		sac1, svari := RollEWS(synRets, p.EwsWin)
		ssplit := split
		sacThr := pct(sac1[ssplit/2:ssplit], p.AcPct)
		svarThr := pct(svari[ssplit/2:ssplit], p.VarPct)
		_ = ssplit
		// the synthetic test loop
		eq2 := 1.0
		pos2 := Long
		en2, enT2 := syn[0].Open, syn[0].Time // the base is long from bar 0
		for i := 1; i < len(syn); i++ {
			gj := split - 1 + i
			sig2 := Long
			if gj < len(sac1) && sac1[gj] > sacThr && svari[gj] > svarThr {
				sig2 = Flat
			}
			if p.EvtOn && killThr > 0 && gj >= p.EvtWin {
				win := synRets[max(0, gj-p.EvtWin):gj]
				losses := make([]float64, len(win))
				for kk, r := range win {
					losses[kk] = -r
				}
				u := percentile(losses, 0.95)
				f := FitPOT(losses, u)
				if f.Excess > 20 && f.ExpectedShortfall(len(win), 0.99) > killThr {
					sig2 = Flat
				}
			}
			if sig2 != pos2 {
				if pos2 != Flat {
					exit := syn[i].Open
					raw := (exit - en2) / en2
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
		// the synthetic buyhold
		bEq2 := 1.0
		bE2 := syn[0].Open
		for i := 1; i < len(syn); i++ {
			raw := (syn[i].Close - bE2) / bE2
			bEq2 = bEq2 * (1 + raw*leverage)
		}
		synImp := (eq2-1)*100 - (bEq2-1)*100
		if synImp >= o.NetImprove {
			better++
		}
	}
	o.MCBetter = better
	o.MCProb = float64(better) / float64(mcN)
	return o
}
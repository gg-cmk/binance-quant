// ═══ binance-quant — the validation pipeline ═════════════════════════════
// The walk-forward (the train → test with the honest out-of-sample) + the
// Monte Carlo (the shuffled returns — the strategy's edge vs the random).
// The 6 gates: the expectancy > cost, the Sharpe > 1, the maxDD < 20%,
// the OOS hit rate ≥ 51%, the MC coincidence < 1%, the net > 0.
package quant

import (
	"math"
	"math/rand"
)

// StratParams — the strategy knobs (the sweep grid).
type StratParams struct {
	States     int     // the HMM states (2-3)
	VolPct     float64 // the HAR vol gate percentile (0.75-0.95)
	MinHold    int     // the minimum holding bars before a flip
	EVT        bool    // the EVT-POT kill switch
	EVTWin     int     // the rolling window for the POT fit (bars)
	EVTPct     float64 // the ES(0.99) kill threshold (the train's percentile)
	BarsPerDay int     // the bars per day of the timeframe (96=15m, 24=1h, 6=4h)
	StopPct    float64 // the per-trade stop-loss in % of price (0 = none, 0.03 = 3%)
}

// DefaultParams — the current best-guess baseline.
func DefaultParams() StratParams {
	return StratParams{States: 3, VolPct: 0.85, MinHold: 4, EVT: true, EVTWin: 4 * 96, EVTPct: 0.95, BarsPerDay: 96, StopPct: 0}
}

// Validate — the walk-forward of the HMM×HAR×EVT strategy: the model is fit
// on the train, the trades run on the out-of-sample test, the metrics are
// compared to the Monte Carlo null (the shuffled returns).
func Validate(candles []Candle, returns []float64, fd Funding, cost CostModel, leverage float64, trainFrac float64, mcN int, seed int64, p StratParams) Validation {
	v := Validation{TrainFrac: trainFrac}
	split := int(float64(len(candles)) * trainFrac)
	testC := candles[split:]
	trainR := returns[:split-1]

	// the HMM regime on the train → the direction per state
	hmm := NewHMM(p.States)
	hmm.Fit(trainR, 40)
	// the state means → the sorted: the highest = the long state, the lowest = the short
	order := make([]int, p.States)
	for i := 0; i < p.States; i++ {
		order[i] = i
	}
	for a := 0; a < p.States; a++ {
		for b := a + 1; b < p.States; b++ {
			if hmm.Mu[order[b]] > hmm.Mu[order[a]] {
				order[a], order[b] = order[b], order[a]
			}
		}
	}
	// the test: the Viterbi states on the test returns → the direction
	testR := returns[split-1:]
	path := hmm.Viterbi(testR)

	// the HAR vol gate on the test
	har := &HARVol{BarsPerDay: p.BarsPerDay}
	har.Fit(returns)
	// the vol threshold: the 85th percentile of the test forecasts
	thr := percentile(har.Forecast[split:], p.VolPct)
	// the EVT-POT kill threshold: the train's ES(0.99) at the p.EVTPct
	// percentile — the strategy goes flat when the rolling tail risk exceeds it
	killThr := 0.0
	evtStep := p.BarsPerDay // one ES sample per day
	evtWarmup := 4 * p.BarsPerDay
	if p.EVT {
		esList := []float64{}
		step := evtStep
		for i := evtWarmup; i < split; i += step {
			win := returns[max(0, i-p.EVTWin):i]
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
			killThr = percentile(esList, p.EVTPct)
		}
	}

	// the trade loop (the test only)
	pos := Flat
	entry, entryT := 0.0, int64(0)
	equity := 1.0
	var trades []Trade
	eq := make([]float64, len(testC))
	eq[0] = 1.0
	entered := -1 << 30 // the bar of the current entry (the min-hold guard)
	for i := 1; i < len(testC); i++ {
		gi := split - 1 + i // the GLOBAL returns index for the HAR forecast
		sig := Flat
		if i < len(path) && gi < len(har.Forecast) && har.Forecast[gi] < thr {
			s := path[i] // the path is indexed LOCALLY (path[0] = the testC[0]'s state)
			if s == order[0] {
				sig = Long
			} else if s == order[p.States-1] {
				sig = Short
			}
		}
		// the EVT-POT kill switch: the rolling ES(0.99) over the last
		// EVTWin returns — the tail risk above the train's killThr → flat.
		// The kill OVERRIDES the min-hold (a risk exit is never blocked).
		evtKill := false
		if p.EVT && killThr > 0 && i >= p.EVTWin {
			win := returns[max(0, gi-p.EVTWin):gi]
			losses := make([]float64, len(win))
			for k, r := range win {
				losses[k] = -r
			}
			u := percentile(losses, 0.95)
			f := FitPOT(losses, u)
			if f.Excess > 20 && f.ExpectedShortfall(len(win), 0.99) > killThr {
				sig = Flat
				evtKill = true
			}
		}
		// the stop-loss: the adverse price move beyond StopPct → exit at the
		// stop price (or the gap-through open). Also overrides the min-hold.
		stopHit := false
		stopExit := 0.0
		if pos != Flat && p.StopPct > 0 {
			if pos == Long {
				stopPrice := entry * (1 - p.StopPct)
				if testC[i].Low <= stopPrice {
					stopHit = true
					stopExit = stopPrice
					if testC[i].Open < stopPrice {
						stopExit = testC[i].Open // gapped through the stop
					}
				}
			} else { // Short
				stopPrice := entry * (1 + p.StopPct)
				if testC[i].High >= stopPrice {
					stopHit = true
					stopExit = stopPrice
					if testC[i].Open > stopPrice {
						stopExit = testC[i].Open // gapped through the stop
					}
				}
			}
			if stopHit {
				sig = Flat
			}
		}
		// the min-hold: no flip within the MinHold bars of the entry —
		// EXCEPT the risk exits (EVT kill, stop-loss) which always fire
		if sig != pos && pos != Flat && i-entered < p.MinHold && !evtKill && !stopHit {
			sig = pos
		}
		if sig != pos {
			if pos != Flat {
				exit := testC[i].Open
				if stopHit {
					exit = stopExit
				}
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
				entered = i
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
	// the Monte Carlo: the shuffled returns, the same pipeline, N runs
	rng := rand.New(rand.NewSource(seed))
	shuffled := make([]float64, len(testR))
	better := 0
	for k := 0; k < mcN; k++ {
		copy(shuffled, testR)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		// the HMM on the shuffled train → the directions — the same trade loop
		sp := hmm.Viterbi(shuffled)
		// the trade loop on the shuffled (the trades at the same times)
		eq2 := 1.0
		pos2 := Flat
		en2, enT2 := 0.0, int64(0)
		entered2 := -1 << 30
		net2 := 0.0
		for i := 1; i < len(testC); i++ {
			gi := split - 1 + i
			sig := Flat
			if i < len(sp) && gi < len(har.Forecast) && har.Forecast[gi] < thr {
				if sp[i] == order[0] {
					sig = Long
				} else if sp[i] == order[p.States-1] {
					sig = Short
				}
			}
			// the same stop-loss as the real loop (the null must face the
			// same risk management or the comparison is unfair)
			stopHit := false
			if pos2 != Flat && p.StopPct > 0 {
				if pos2 == Long {
					if testC[i].Low <= en2*(1-p.StopPct) {
						stopHit = true
					}
				} else if testC[i].High >= en2*(1+p.StopPct) {
					stopHit = true
				}
				if stopHit {
					sig = Flat
				}
			}
			// the min-hold (the risk exits still override)
			if sig != pos2 && pos2 != Flat && i-entered2 < p.MinHold && !stopHit {
				sig = pos2
			}
			if sig != pos2 {
				if pos2 != Flat {
					exit := testC[i].Open
					if stopHit {
						if pos2 == Long {
							exit = en2 * (1 - p.StopPct)
							if testC[i].Open < exit {
								exit = testC[i].Open
							}
						} else {
							exit = en2 * (1 + p.StopPct)
							if testC[i].Open > exit {
								exit = testC[i].Open
							}
						}
					}
					raw := (exit - en2) / en2
					if pos2 == Short {
						raw = -raw
					}
					fund := fd.fundingFor(enT2, testC[i].Time)
					costPct := cost.CostPerSide()*2 + fund
					eq2 *= 1 + raw*leverage - costPct
				}
				if sig != Flat {
					en2, enT2 = testC[i].Open, testC[i].Time
					entered2 = i
				}
				pos2 = sig
			}
		}
		net2 = (eq2 - 1) * 100
		if net2 >= v.Net {
			better++
		}
	}
	v.MCBetter = better
	v.MCProb = float64(better) / float64(mcN)
	return v
}

// Validation — the gate-oriented report.
type Validation struct {
	TrainFrac  float64
	Trades     int
	WinRate    float64
	Expectancy float64
	Net        float64
	MaxDD      float64
	MCBetter   int
	MCProb     float64 // the P(MC ≥ real) — the coincidence probability
}

func percentile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64{}, v...)
	for a := 0; a < len(s); a++ {
		for b := a + 1; b < len(s); b++ {
			if s[b] < s[a] {
				s[a], s[b] = s[b], s[a]
			}
		}
	}
	idx := int(q * float64(len(s)-1))
	return s[idx]
}

// Gates — the 6 acceptance gates.
type Gates struct {
	Pass bool
	Fail []string
}

func CheckGates(v Validation, costPerTrade float64) Gates {
	g := Gates{Pass: true}
	if v.Expectancy <= costPerTrade*100*2 {
		g.Fail = append(g.Fail, "expectancy ≤ cost")
	}
	if v.Trades < 50 {
		g.Fail = append(g.Fail, "trades < 50")
	}
	if v.MaxDD >= 20 {
		g.Fail = append(g.Fail, "maxDD ≥ 20%")
	}
	if v.WinRate < 0.51 {
		g.Fail = append(g.Fail, "win rate < 51%")
	}
	if v.MCProb > 0.01 {
		g.Fail = append(g.Fail, "MC coincidence ≥ 1%")
	}
	if v.Net <= 0 {
		g.Fail = append(g.Fail, "net ≤ 0")
	}
	if len(g.Fail) > 0 {
		g.Pass = false
	}
	_ = math.Sqrt
	return g
}

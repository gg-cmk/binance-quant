// ═══ binance-quant — the net-vs-expectancy decomposition ═════════════════
// Why is the expectancy positive but the net negative? The answer: the net
// includes the LAST open position marked to market. This breaks it apart.
package quant

// DecomposeResult — the closed vs open P&L split.
type DecomposeResult struct {
	ClosedPct      float64
	Closed         int
	OpenPct        float64
	OpenDir        int
	Net            float64
	MaxDD          float64
	KillTriggers   int
	KillSuppressed int
	Flips          int
	TradeNets      []float64
	LongPct        float64
	LongN          int
	ShortPct       float64
	ShortN         int
	StopOuts       int
}

// Decompose — the same trade loop as Validate, but reports the P&L
// decomposition and the kill-switch suppression count.
func Decompose(candles []Candle, returns []float64, fd Funding, cost CostModel, leverage, trainFrac float64, p StratParams) DecomposeResult {
	d := DecomposeResult{}
	split := int(float64(len(candles)) * trainFrac)
	testC := candles[split:]
	trainR := returns[:split-1]

	hmm := NewHMM(p.States)
	hmm.Fit(trainR, 40)
	order := make([]int, p.States)
	for i := range order {
		order[i] = i
	}
	for a := 0; a < p.States; a++ {
		for b := a + 1; b < p.States; b++ {
			if hmm.Mu[order[b]] > hmm.Mu[order[a]] {
				order[a], order[b] = order[b], order[a]
			}
		}
	}
	testR := returns[split-1:]
	path := hmm.Viterbi(testR)

	har := &HARVol{BarsPerDay: p.BarsPerDay}
	har.Fit(returns)
	thr := percentile(har.Forecast[split:], p.VolPct)

	killThr := 0.0
	if p.EVT {
		esList := []float64{}
		evtWarmup := 4 * p.BarsPerDay
		for i := evtWarmup; i < split; i += p.BarsPerDay {
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

	pos := Flat
	entry, entryT := 0.0, int64(0)
	equity := 1.0
	entered := -1 << 30
	closedPct := 0.0
	eq := make([]float64, len(testC))
	eq[0] = 1.0
	for i := 1; i < len(testC); i++ {
		gi := split - 1 + i
		sig := Flat
		if i < len(path) && gi < len(har.Forecast) && har.Forecast[gi] < thr {
			s := path[i]
			if s == order[0] {
				sig = Long
			} else if s == order[p.States-1] {
				sig = Short
			}
		}
		killFired := false
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
				killFired = true
			}
		}
		if killFired {
			d.KillTriggers++
			if pos != Flat && i-entered < p.MinHold {
				d.KillSuppressed++
			}
		}
		// the stop-loss (mirror of Validate): the adverse move beyond StopPct
		stopHit := false
		stopExit := 0.0
		if pos != Flat && p.StopPct > 0 {
			if pos == Long {
				stopPrice := entry * (1 - p.StopPct)
				if testC[i].Low <= stopPrice {
					stopHit = true
					stopExit = stopPrice
					if testC[i].Open < stopPrice {
						stopExit = testC[i].Open
					}
				}
			} else {
				stopPrice := entry * (1 + p.StopPct)
				if testC[i].High >= stopPrice {
					stopHit = true
					stopExit = stopPrice
					if testC[i].Open > stopPrice {
						stopExit = testC[i].Open
					}
				}
			}
			if stopHit {
				sig = Flat
			}
		}
		if sig != pos && pos != Flat && i-entered < p.MinHold && !killFired && !stopHit {
			sig = pos
		}
		if sig != pos {
			if pos != Flat {
				exit := testC[i].Open
				if stopHit {
					exit = stopExit
					d.StopOuts++
				}
				raw := (exit - entry) / entry
				if pos == Short {
					raw = -raw
				}
				fund := fd.fundingFor(entryT, testC[i].Time)
				costPct := cost.CostPerSide()*2 + fund
				net := raw*leverage - costPct
				equity *= 1 + net
				closedPct += net * 100
				d.Closed++
				d.TradeNets = append(d.TradeNets, net*100)
				if pos == Long {
					d.LongPct += net * 100
					d.LongN++
				} else {
					d.ShortPct += net * 100
					d.ShortN++
				}
			}
			if sig != Flat {
				entry, entryT = testC[i].Open, testC[i].Time
				entered = i
			}
			d.Flips++
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
	d.ClosedPct = closedPct
	d.Net = (eq[len(eq)-1] - 1) * 100
	// the final open position marked to market
	if pos != Flat {
		raw := (testC[len(testC)-1].Close - entry) / entry
		if pos == Short {
			raw = -raw
		}
		d.OpenPct = raw * leverage * 100
		d.OpenDir = int(pos)
	}
	peak := eq[0]
	for _, e := range eq {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := (peak - e) / peak * 100
			if dd > d.MaxDD {
				d.MaxDD = dd
			}
		}
	}
	return d
}

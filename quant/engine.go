// ═══ binance-quant — the backtest core ═══════════════════════════════════
// Design contract (user-approved 2026-08-17, 2x perpetual futures):
//
//	Candle   — the OHLCV bar (ms openTime)
//	Cost     — taker 0.05% × 2 (round trip) + slippage 8bp + the 8h funding
//	Position — -1 / 0 / +1 at a FIXED 2x notional
//	Metrics  — expectancy, Sharpe, maxDD, hit rate — the 6-gate inputs
//
// The strategy interface: Signal(candle, ctx) → Position. A benchmark beats
// the gate or is discarded (the honest expectation: direction ≈ 50-55%).
package quant

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"time"
)

// Candle — one OHLCV bar. Time is the UTC ms open of the bar.
type Candle struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// CostModel — the friction the backtest applies on every fill.
type CostModel struct {
	TakerPct    float64 // the taker fee per side (futures: 0.0005)
	SlippagePct float64 // the slippage per side (8bp = 0.0008)
}

// CostPerSide — the total friction per fill (one side).
func (c CostModel) CostPerSide() float64 { return c.TakerPct + c.SlippagePct }

// LoadCandles — the data CSV → candles (skip the header, ms openTime).
func LoadCandles(path string) ([]Candle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	out := []Candle{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) < 6 {
			continue
		}
		ms, err1 := strconv.ParseInt(rec[0], 10, 64)
		o, err2 := strconv.ParseFloat(rec[1], 64)
		h, err3 := strconv.ParseFloat(rec[2], 64)
		l, err4 := strconv.ParseFloat(rec[3], 64)
		c, err5 := strconv.ParseFloat(rec[4], 64)
		v, err6 := strconv.ParseFloat(rec[5], 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
			continue
		}
		out = append(out, Candle{Time: ms, Open: o, High: h, Low: l, Close: c, Volume: v})
	}
	return out, nil
}

// Position — the strategy's stance at a bar.
type Position int

const (
	Flat  Position = 0
	Long  Position = 1
	Short Position = -1
)

// Strategy — the model contract. The ctx carries the rolling history so the
// strategy can compute the regime/volatility signals from the past bars.
type Strategy interface {
	Signal(i int, candles []Candle, ctx *StratCtx) Position
	Name() string
}

// StratCtx — the strategy-side state (the indicators, the regime labels).
type StratCtx struct {
	// the strategy owns whatever it needs across bars
	Data map[string]float64
}

func NewStratCtx() *StratCtx { return &StratCtx{Data: map[string]float64{}} }

// Trade — one opened position (the fill info for the metrics).
type Trade struct {
	OpenI      int
	CloseI     int
	Dir        Position
	Entry      float64
	Exit       float64
	RetPct     float64 // the raw return % of the price move (pre-leverage)
	CostPct    float64 // the total friction % (both fills + the funding)
	FundingPct float64 // the funding paid while open
}

// Result — the backtest outcome.
type Result struct {
	Strategy   string
	Candles    int
	Trades     int
	WinRate    float64
	Expectancy float64 // the mean net % per trade (post-cost, post-funding, post-leverage)
	Sharpe     float64 // the annualized Sharpe of the per-bar equity returns
	MaxDD      float64 // the max drawdown of the equity curve %
	NetPct     float64 // the total net return % of the equity
	Equity     []float64
}

// FundingRate — the 8h funding: time → rate. Applied to the open position
// at the funding timestamps (a pro-rata share for the bars).
type Funding struct {
	Times []int64
	Rates []float64
}

// LoadFunding — the funding CSV → the funding schedule.
func LoadFunding(path string) (Funding, error) {
	f, err := os.Open(path)
	if err != nil {
		return Funding{}, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	var out Funding
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 3 {
			continue
		}
		ms, err1 := strconv.ParseInt(rec[0], 10, 64)
		r, err2 := strconv.ParseFloat(rec[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out.Times = append(out.Times, ms)
		out.Rates = append(out.Rates, r)
	}
	return out, nil
}

// fundingFor — the funding accrued between the open and the close of a
// trade (the 8h timestamps in the window, the rate × the days held ratio).
func (fd Funding) fundingFor(openT, closeT int64) float64 {
	total := 0.0
	for k, t := range fd.Times {
		if t >= openT && t < closeT {
			total += fd.Rates[k]
		}
	}
	return total
}

// Run — the backtest loop: the strategy's stance per bar → the fills at the
// close → the costs + the funding → the equity at a fixed 2x.
func Run(candles []Candle, strat Strategy, cost CostModel, fd Funding, leverage float64) Result {
	res := Result{Strategy: strat.Name(), Candles: len(candles), Equity: make([]float64, len(candles))}
	ctx := NewStratCtx()
	pos := Flat
	entry := 0.0
	entryT := int64(0)
	entryI := 0
	equity := 1.0
	trades := []Trade{}
	res.Equity[0] = 1.0
	for i := 1; i < len(candles); i++ {
		sig := strat.Signal(i, candles, ctx)
		// the exit (or the flip) at the close of bar i-1 → the fill at candle[i].Open
		if sig != pos {
			if pos != Flat {
				// close the position
				exit := candles[i].Open
				raw := (exit - entry) / entry
				if pos == Short {
					raw = -raw
				}
				fund := fd.fundingFor(entryT, candles[i].Time)
				costPct := cost.CostPerSide()*2 + fund
				net := raw*leverage - costPct
				equity *= 1 + net
				trades = append(trades, Trade{OpenI: entryI, CloseI: i, Dir: pos, Entry: entry, Exit: exit, RetPct: raw * 100, CostPct: costPct * 100, FundingPct: fund * 100})
			}
			if sig != Flat {
				entry = candles[i].Open
				entryT = candles[i].Time
				entryI = i
			}
			pos = sig
		}
		// the MARK-TO-MARKET: the equity tracks the open position every bar
		if pos != Flat {
			raw := (candles[i].Close - entry) / entry
			if pos == Short {
				raw = -raw
			}
			res.Equity[i] = equity * (1 + raw*leverage)
		} else {
			res.Equity[i] = equity
		}
	}
	// the trade accounting
	res.Trades = len(trades)
	if len(trades) > 0 {
		wins := 0
		sum := 0.0
		for _, t := range trades {
			net := t.RetPct*leverage - t.CostPct
			sum += net
			if net > 0 {
				wins++
			}
		}
		res.WinRate = float64(wins) / float64(len(trades))
		res.Expectancy = sum / float64(len(trades))
	}
	// compute the metrics
	// the per-bar returns
	rets := make([]float64, 0, len(res.Equity))
	for i := 1; i < len(res.Equity); i++ {
		rets = append(rets, res.Equity[i]/res.Equity[i-1]-1)
	}
	// the Sharpe: the mean/std of the per-bar returns × sqrt(bars per year)
	mean, sd := 0.0, 0.0
	if len(rets) > 1 {
		for _, r := range rets {
			mean += r
		}
		mean /= float64(len(rets))
		for _, r := range rets {
			sd += (r - mean) * (r - mean)
		}
		sd = math.Sqrt(sd / float64(len(rets)-1))
	}
	barPerYear := 4.0 * 24.0 * 365.0 // 15m bars
	if sd > 0 {
		res.Sharpe = mean / sd * math.Sqrt(barPerYear)
	}
	res.NetPct = (res.Equity[len(res.Equity)-1] - 1) * 100
	// the max drawdown
	peak := res.Equity[0]
	for _, e := range res.Equity {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := (peak - e) / peak * 100
			if dd > res.MaxDD {
				res.MaxDD = dd
			}
		}
	}
	return res
}

// Print — the gate-oriented report.
func (r Result) Print() {
	fmt.Printf("STRATEGY  %s\n", r.Strategy)
	fmt.Printf("candles   %d\n", r.Candles)
	fmt.Printf("trades    %d\n", r.Trades)
	fmt.Printf("win rate  %.1f%%\n", r.WinRate*100)
	fmt.Printf("expectancy %.4f%% / trade\n", r.Expectancy)
	fmt.Printf("sharpe    %.2f\n", r.Sharpe)
	fmt.Printf("max DD    %.1f%%\n", r.MaxDD)
	fmt.Printf("net       %.2f%%\n", r.NetPct)
	_ = time.Now
}

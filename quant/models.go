// ═══ binance-quant — HAR-RV (Corsi 2009) + EVT-POT ══════════════════════
package quant

import (
	"math"
	"sort"
)

// RealizedVol — the realized volatility of the returns over the window.
func RealizedVol(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}
	s := 0.0
	for _, r := range returns {
		s += r * r
	}
	return math.Sqrt(s / float64(len(returns)))
}

// LogReturns — the per-bar log returns of the closes.
func LogReturns(candles []Candle) []float64 {
	out := make([]float64, 0, len(candles))
	for i := 1; i < len(candles); i++ {
		if candles[i-1].Close > 0 {
			out = append(out, math.Log(candles[i].Close/candles[i-1].Close))
		}
	}
	return out
}

// HARVol — the Heterogeneous AutoRegressive forecast of the volatility
// (Corsi 2009): σ̂(t+1) = β0 + β1·RV_d + β2·RV_w + β3·RV_m — the daily,
// weekly, and monthly realized volatilities. The forecast is a float ≥ 0.
// The bars-per-day scales the windows (the strategy feeds the per-bar data).
type HARVol struct {
	BarsPerDay int
	Daily      []float64 // the RV_d series (per the bar aggregation)
	Weekly     []float64
	Monthly    []float64
	Forecast   []float64 // σ̂(t+1) per bar
}

// Fit — the OLS on the RV windows → the forecast series.
func (h *HARVol) Fit(returns []float64) {
	n := len(returns)
	bpd := h.BarsPerDay
	if bpd <= 0 {
		bpd = 96 // 15m
	}
	h.Daily = make([]float64, n)
	h.Weekly = make([]float64, n)
	h.Monthly = make([]float64, n)
	h.Forecast = make([]float64, n)
	dW, mW := bpd, 7*bpd
	for i := dW; i < n; i++ {
		h.Daily[i] = RealizedVol(returns[i-dW : i])
		if i >= mW {
			h.Weekly[i] = RealizedVol(returns[i-mW : i])
		} else {
			h.Weekly[i] = RealizedVol(returns[max(0, i-7*bpd):i])
		}
		h.Monthly[i] = RealizedVol(returns[max(0, i-30*bpd):i])
	}
	// the OLS: y = X·β with X = [1, RV_d, RV_w, RV_m] — the closed form
	// β = (XᵀX)⁻¹Xᵀy — the training on the first 70%, applied to all.
	rows := [][]float64{}
	ys := []float64{}
	for i := dW; i < n; i++ {
		if i >= int(float64(n)*0.7) {
			break
		}
		rows = append(rows, []float64{1, h.Daily[i], h.Weekly[i], h.Monthly[i]})
		ys = append(ys, h.Daily[i+1])
	}
	beta := ols(rows, ys)
	if beta == nil {
		return
	}
	for i := dW; i < n; i++ {
		f := beta[0] + beta[1]*h.Daily[i] + beta[2]*h.Weekly[i] + beta[3]*h.Monthly[i]
		if f < 0 {
			f = 0
		}
		h.Forecast[i] = f
	}
}

// ols — the normal-equations least squares (4 regressors).
func ols(X [][]float64, y []float64) []float64 {
	if len(X) < 5 {
		return nil
	}
	k := 4
	XtX := make([][]float64, k)
	Xty := make([]float64, k)
	for a := 0; a < k; a++ {
		XtX[a] = make([]float64, k)
		for b := 0; b < k; b++ {
			s := 0.0
			for i := range X {
				s += X[i][a] * X[i][b]
			}
			XtX[a][b] = s
		}
		s := 0.0
		for i := range X {
			s += X[i][a] * y[i]
		}
		Xty[a] = s
	}
	// the Gaussian elimination
	aug := make([][]float64, k)
	for a := 0; a < k; a++ {
		aug[a] = append(append([]float64{}, XtX[a]...), Xty[a])
	}
	for col := 0; col < k; col++ {
		p := col
		for r := col + 1; r < k; r++ {
			if math.Abs(aug[r][col]) > math.Abs(aug[p][col]) {
				p = r
			}
		}
		aug[col], aug[p] = aug[p], aug[col]
		if math.Abs(aug[col][col]) < 1e-12 {
			return nil
		}
		for r := 0; r < k; r++ {
			if r == col {
				continue
			}
			f := aug[r][col] / aug[col][col]
			for c := col; c <= k; c++ {
				aug[r][c] -= f * aug[col][c]
			}
		}
	}
	beta := make([]float64, k)
	for r := 0; r < k; r++ {
		beta[r] = aug[r][k] / aug[r][r]
	}
	return beta
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ══ EVT-POT (peaks-over-threshold, the GPD tail) ══════════════════════════
// The negative returns (the losses) above the threshold u → the Generalized
// Pareto fit → the VaR/ES at the tail quantiles. The 2x futures kill-switch
// input: when the predicted tail risk spikes, the strategy goes flat.

// GPDFit — the Pickands-Balkema-de Haan fit (the moment estimators).
type GPDFit struct {
	Threshold float64
	Xi        float64 // the shape
	Beta      float64 // the scale
	Excess    int
	Mean      float64 // the mean excess
}

// FitPOT — the peaks over the threshold u (e.g. the 95th percentile of the
// losses). The shape/scale from the method-of-moments on the excesses.
func FitPOT(losses []float64, u float64) GPDFit {
	exc := []float64{}
	for _, l := range losses {
		if l >= u {
			exc = append(exc, l-u)
		}
	}
	f := GPDFit{Threshold: u, Excess: len(exc)}
	if len(exc) < 20 {
		return f
	}
	sort.Float64s(exc)
	m1, m2 := 0.0, 0.0
	for _, e := range exc {
		m1 += e
		m2 += e * e
	}
	m1 /= float64(len(exc))
	m2 /= float64(len(exc))
	f.Mean = m1
	// the moment estimators: xi = 0.5·(1 − m1²/m2); beta = m1·(1 − xi)
	xi := 0.5 * (1 - m1*m1/m2)
	if xi >= 0.5 {
		xi = 0.45
	}
	f.Xi = xi
	f.Beta = m1 * (1 - xi)
	if f.Beta < 1e-9 {
		f.Beta = m1 * 0.5
	}
	return f
}

// Var — the GPD VaR at the tail probability q (e.g. 0.99).
func (f GPDFit) Var(n int, q float64) float64 {
	if f.Excess < 20 || f.Beta <= 0 {
		return 0
	}
	// the POT VaR: u + (β/ξ)·(((n/N)·(1−q))^(−ξ) − 1)
	ratio := float64(n) / float64(f.Excess)
	term := math.Pow(ratio*(1-q), -f.Xi)
	return f.Threshold + (f.Beta/f.Xi)*(term-1)
}

// ExpectedShortfall — the ES (the conditional tail mean) at q.
func (f GPDFit) ExpectedShortfall(n int, q float64) float64 {
	v := f.Var(n, q)
	if v <= 0 || f.Xi >= 1 {
		return v
	}
	// ES = VaR/(1−ξ) + (β−ξ·u)/(1−ξ)
	return v/(1-f.Xi) + (f.Beta-f.Xi*f.Threshold)/(1-f.Xi)
}

// ═══ binance-quant — the Gaussian HMM (Baum-Welch + Viterbi) ════════════
// The regime detection (the L1 layer): the per-bar log returns → the states
// with the Gaussian emissions — e.g. N=3: the down/up trends + the sideways.
// The EM (Baum-Welch) fits the transition matrix + the state means/variances;
// the Viterbi decodes the most likely state path.
package quant

import "math"

// HMM — a Gaussian-emission HMM over the returns.
type HMM struct {
	N      int
	Pi     []float64
	A      [][]float64
	Mu     []float64
	Sigma  []float64
	States []int // the Viterbi path
}

// NewHMM — the states with the data-scaled initial means.
func NewHMM(n int) *HMM {
	return &HMM{N: n}
}

// Fit — the Baum-Welch EM on the log returns.
func (h *HMM) Fit(rets []float64, iters int) {
	n := h.N
	if n < 2 {
		n = 3
		h.N = 3
	}
	// the data stats for the init
	mean, sd := 0.0, 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	for _, r := range rets {
		sd += (r - mean) * (r - mean)
	}
	sd = math.Sqrt(sd / float64(len(rets)))
	if sd < 1e-12 {
		sd = 1e-4
	}
	// the init: the means spread across the return range, the equal transitions
	h.Mu = make([]float64, n)
	h.Sigma = make([]float64, n)
	h.Pi = make([]float64, n)
	h.A = make([][]float64, n)
	for i := 0; i < n; i++ {
		h.Mu[i] = mean + sd*(float64(i)-(float64(n)-1)/2)*1.2
		h.Sigma[i] = sd
		h.Pi[i] = 1.0 / float64(n)
		h.A[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			h.A[i][j] = 1.0 / float64(n)
		}
	}
	T := len(rets)
	for it := 0; it < iters; it++ {
		// the forward-backward (scaled)
		alpha := make([][]float64, T)
		beta := make([][]float64, T)
		scale := make([]float64, T)
		b := make([][]float64, T)
		for t := 0; t < T; t++ {
			b[t] = make([]float64, n)
			for i := 0; i < n; i++ {
				b[t][i] = gauss(rets[t], h.Mu[i], h.Sigma[i])
			}
			alpha[t] = make([]float64, n)
			beta[t] = make([]float64, n)
		}
		// forward
		for i := 0; i < n; i++ {
			alpha[0][i] = h.Pi[i] * b[0][i]
		}
		scale[0] = sumVec(alpha[0])
		if scale[0] < 1e-300 {
			scale[0] = 1e-300
		}
		for i := 0; i < n; i++ {
			alpha[0][i] /= scale[0]
		}
		for t := 1; t < T; t++ {
			for j := 0; j < n; j++ {
				s := 0.0
				for i := 0; i < n; i++ {
					s += alpha[t-1][i] * h.A[i][j]
				}
				alpha[t][j] = s * b[t][j]
			}
			scale[t] = sumVec(alpha[t])
			if scale[t] < 1e-300 {
				scale[t] = 1e-300
			}
			for j := 0; j < n; j++ {
				alpha[t][j] /= scale[t]
			}
		}
		// backward
		for i := 0; i < n; i++ {
			beta[T-1][i] = 1
		}
		for t := T - 2; t >= 0; t-- {
			for i := 0; i < n; i++ {
				s := 0.0
				for j := 0; j < n; j++ {
					s += h.A[i][j] * b[t+1][j] * beta[t+1][j]
				}
				beta[t][i] = s / scale[t+1]
			}
		}
		// the re-estimation (the posteriors)
		gamma := make([][]float64, T)
		xi := make([][][]float64, T-1)
		for t := 0; t < T; t++ {
			gamma[t] = make([]float64, n)
			s := 0.0
			for i := 0; i < n; i++ {
				gamma[t][i] = alpha[t][i] * beta[t][i]
				s += gamma[t][i]
			}
			if s < 1e-300 {
				s = 1e-300
			}
			for i := 0; i < n; i++ {
				gamma[t][i] /= s
			}
		}
		for t := 0; t < T-1; t++ {
			xi[t] = make([][]float64, n)
			s := 0.0
			for i := 0; i < n; i++ {
				xi[t][i] = make([]float64, n)
				for j := 0; j < n; j++ {
					xi[t][i][j] = alpha[t][i] * h.A[i][j] * b[t+1][j] * beta[t+1][j]
					s += xi[t][i][j]
				}
			}
			if s < 1e-300 {
				s = 1e-300
			}
			for i := 0; i < n; i++ {
				for j := 0; j < n; j++ {
					xi[t][i][j] /= s
				}
			}
		}
		// the parameters
		newPi := make([]float64, n)
		newA := make([][]float64, n)
		newMu := make([]float64, n)
		newSig := make([]float64, n)
		for i := 0; i < n; i++ {
			newA[i] = make([]float64, n)
		}
		for i := 0; i < n; i++ {
			newPi[i] = gamma[0][i]
			for j := 0; j < n; j++ {
				num, den := 0.0, 0.0
				for t := 0; t < T-1; t++ {
					num += xi[t][i][j]
					den += gamma[t][i]
				}
				if den > 1e-300 {
					newA[i][j] = num / den
				} else {
					newA[i][j] = 1.0 / float64(n)
				}
			}
			numM, denM := 0.0, 0.0
			for t := 0; t < T; t++ {
				numM += gamma[t][i] * rets[t]
				denM += gamma[t][i]
			}
			if denM > 1e-300 {
				newMu[i] = numM / denM
			}
			numV := 0.0
			for t := 0; t < T; t++ {
				d := rets[t] - newMu[i]
				numV += gamma[t][i] * d * d
			}
			if denM > 1e-300 {
				newSig[i] = math.Sqrt(numV / denM)
			} else {
				newSig[i] = sd
			}
			if newSig[i] < 1e-6 {
				newSig[i] = 1e-6
			}
		}
		h.Pi, h.A, h.Mu, h.Sigma = newPi, newA, newMu, newSig
	}
	h.States = h.Viterbi(rets)
}

// Viterbi — the most likely state path.
func (h *HMM) Viterbi(rets []float64) []int {
	T := len(rets)
	n := h.N
	delta := make([][]float64, T)
	psi := make([][]int, T)
	for t := 0; t < T; t++ {
		delta[t] = make([]float64, n)
		psi[t] = make([]int, n)
	}
	for i := 0; i < n; i++ {
		delta[0][i] = math.Log(h.Pi[i]+1e-300) + math.Log(gauss(rets[0], h.Mu[i], h.Sigma[i])+1e-300)
	}
	for t := 1; t < T; t++ {
		for j := 0; j < n; j++ {
			best, bi := -1e300, 0
			for i := 0; i < n; i++ {
				v := delta[t-1][i] + math.Log(h.A[i][j]+1e-300)
				if v > best {
					best, bi = v, i
				}
			}
			delta[t][j] = best + math.Log(gauss(rets[t], h.Mu[j], h.Sigma[j])+1e-300)
			psi[t][j] = bi
		}
	}
	path := make([]int, T)
	best, bi := -1e300, 0
	for i := 0; i < n; i++ {
		if delta[T-1][i] > best {
			best, bi = delta[T-1][i], i
		}
	}
	path[T-1] = bi
	for t := T - 2; t >= 0; t-- {
		path[t] = psi[t+1][path[t+1]]
	}
	return path
}

func gauss(x, mu, sd float64) float64 {
	d := (x - mu) / sd
	return math.Exp(-0.5*d*d) / (sd * math.Sqrt(2*math.Pi))
}

func sumVec(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s
}

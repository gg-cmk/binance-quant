// ═══ binance-quant — the HMM state mu diagnostic ═════════════════════════
// What drift does each HMM state actually carry? The direction assignment
// uses ordinal ranking — but if the "best" state's mu is still negative or
// below the cost hurdle, long trades are structurally unprofitable.
// This prints the fitted mu/sigma and the state occupation counts.
package main

import (
	"fmt"

	"binance-quant/quant"
)

func main() {
	candles, _ := quant.LoadCandles("data/BTCUSDT-1h-futures.csv")
	rets := quant.LogReturns(candles)
	for _, states := range []int{2, 3} {
		split := int(float64(len(candles)) * 0.6)
		trainR := rets[:split-1]
		hmm := quant.NewHMM(states)
		hmm.Fit(trainR, 40)
		// the global cost hurdle per bar: 0.26% round trip / ~96-bar hold
		hurdle := 0.0026 / 96
		fmt.Printf("=== %d states (train %d bars) ===\n", states, len(trainR))
		for s := 0; s < states; s++ {
			fmt.Printf("  state %d: mu %+.6f/bar (%.4f%%/day) sigma %.6f | cost-hurdle %+.6f | %s\n",
				s, hmm.Mu[s], hmm.Mu[s]*24*100, hmm.Sigma[s], hurdle,
				map[bool]string{true: "TRADEABLE", false: "below hurdle"}[hmm.Mu[s] > hurdle || hmm.Mu[s] < -hurdle])
		}
		// the Viterbi occupation on the TEST side
		testR := rets[split-1:]
		path := hmm.Viterbi(testR)
		counts := make([]int, states)
		for _, s := range path {
			counts[s]++
		}
		fmt.Printf("  test occupation: %v (of %d bars)\n", counts, len(path))
	}
}
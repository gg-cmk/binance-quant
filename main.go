// ═══ binance-quant web — the status/health service ═══════════════════════
// The deployable surface of the quant project: serves the data summary
// (row count, span, latest close) and a health endpoint. The research
// pipeline (fetch → backtest → validation) stays CLI-side.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type DataSummary struct {
	Symbol      string  `json:"symbol"`
	Interval    string  `json:"interval"`
	Market      string  `json:"market"`
	Leverage    int     `json:"leverage"`
	Rows        int     `json:"rows"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	LatestClose float64 `json:"latestClose"`
	High24h     float64 `json:"high24h"`
	Low24h      float64 `json:"low24h"`
	FundingAvg  float64 `json:"fundingAvgPct"`
	FundingRows int     `json:"fundingRows"`
	DataOK      bool    `json:"dataOK"`
}

func loadSummary() DataSummary {
	s := DataSummary{Symbol: "BTCUSDT", Interval: "15m", Market: "futures-um", Leverage: 2}
	path := os.Getenv("DATA_CSV")
	if path == "" {
		path = filepath.Join("data", "BTCUSDT-15m-futures.csv") // the 2x perpetual futures (user 2026-08-17)
	}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	cr := csv.NewReader(f)
	rows := [][]string{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return s
		}
		if len(rec) < 5 {
			continue
		}
		rows = append(rows, rec)
	}
	if len(rows) < 2 {
		return s
	}
	// the header skip
	data := rows[1:]
	s.Rows = len(data)
	s.From = ts(data[0][0])
	s.To = ts(data[len(data)-1][0])
	if c, err := strconv.ParseFloat(data[len(data)-1][4], 64); err == nil {
		s.LatestClose = c
	}
	// the last 96 candles (a day of 15m) — the high/low
	win := data
	if len(win) > 96 {
		win = win[len(win)-96:]
	}
	for _, r := range win {
		if h, err := strconv.ParseFloat(r[2], 64); err == nil && h > s.High24h {
			s.High24h = h
		}
		if l, err := strconv.ParseFloat(r[3], 64); err == nil && (s.Low24h == 0 || l < s.Low24h) {
			s.Low24h = l
		}
	}
	s.DataOK = true
	fp := os.Getenv("FUNDING_CSV")
	if fp == "" {
		fp = filepath.Join("data", "BTCUSDT-funding.csv")
	}
	if ff, err := os.Open(fp); err == nil {
		defer ff.Close()
		fcr := csv.NewReader(ff)
		sum := 0.0
		n := 0
		for {
			rec, err := fcr.Read()
			if err == io.EOF {
				break
			}
			if err != nil || len(rec) < 3 {
				continue
			}
			if v, err := strconv.ParseFloat(rec[2], 64); err == nil {
				sum += v
				n++
			}
		}
		if n > 0 {
			s.FundingAvg = sum / float64(n) * 100
			s.FundingRows = n
		}
	}
	return s
}

func ts(ms string) string {
	var n int64
	fmt.Sscanf(ms, "%d", &n)
	return time.UnixMilli(n).UTC().Format("2006-01-02 15:04")
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s := loadSummary()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html lang="ko"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>binance-quant</title>
<style>
body{ margin:0; background:#0F1115; color:#E6E8EB; font-family:ui-monospace,Menlo,monospace; }
main{ max-width:560px; margin:0 auto; padding:48px 24px; }
h1{ font-size:22px; letter-spacing:.5px; }
.metric{ display:flex; justify-content:space-between; padding:14px 0; border-bottom:1px solid #23262D; }
.metric b{ font-weight:700; }
.bad{ color:#FF6B6B; } .good{ color:#51CF66; }
</style></head><body><main>
<h1>binance-quant</h1>
<div class="metric"><span>market</span><b>%s · %s / %s · %dx</b></div>
<div class="metric"><span>rows</span><b>%d</b></div>
<div class="metric"><span>span</span><b>%s → %s</b></div>
<div class="metric"><span>latest close</span><b>$%.2f</b></div>
<div class="metric"><span>24h high / low</span><b>$%.2f / $%.2f</b></div>
<div class="metric"><span>funding avg (8h)</span><b>%.4f%% · %d rows</b></div>
<div class="metric"><span>data</span><b class="%s">%s</b></div>
</main></body></html>`,
			s.Market, s.Symbol, s.Interval, s.Leverage, s.Rows, s.From, s.To,
			s.LatestClose, s.High24h, s.Low24h, s.FundingAvg, s.FundingRows,
			map[bool]string{true: "good", false: "bad"}[s.DataOK],
			map[bool]string{true: "loaded", false: "missing — run go run ./cmd/fetch"}[s.DataOK])
	})
	srv := &http.Server{Addr: ":" + port, ReadHeaderTimeout: 10 * time.Second}
	// the Railway router targets the exposed port — bind it directly.
	fmt.Println("binance-quant web on :" + port)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
}

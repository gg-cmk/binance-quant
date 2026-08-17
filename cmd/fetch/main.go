// ═══ binance-quant — the data fetcher ═══════════════════════════════════
// The Binance spot monthly klines from the PUBLIC data mirror
// (data.binance.vision) — the api.binance.com REST is geo-blocked from
// this VM (HTTP 451), but the official data mirror is not. No API key.
//
//	https://data.binance.vision/data/spot/monthly/klines/{SYMBOL}/{INTERVAL}/{SYMBOL}-{INTERVAL}-{YYYY-MM}.zip
//	CSV columns (the Binance monthly format):
//	  openTime,open,high,low,close,volume,closeTime,quoteVolume,trades,
//	  takerBuyBase,takerBuyQuote,ignore
//	openTime = the UTC millisecond open of the candle.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://data.binance.vision/data" // + /spot or /futures/um + /monthly/klines

func main() {
	symbol := "BTCUSDT"
	interval := "15m"
	market := "futures" // the 2x PERPETUAL FUTURES (user 2026-08-17 '2배 선물로 하자') — the USDT-margined futures by default
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Now().UTC()
	outDir := "data"

	if len(os.Args) > 1 {
		symbol = os.Args[1]
	}
	if len(os.Args) > 2 {
		interval = os.Args[2]
	}
	if len(os.Args) > 3 {
		market = os.Args[3] // "spot" | "futures"
	}
	if len(os.Args) > 4 {
		// the start year-month, e.g. "2019-09" (the USDT-M perpetual launch)
		if t, err := time.Parse("2006-01", os.Args[4]); err == nil {
			from = t
		}
	}
	os.MkdirAll(outDir, 0o755)
	path := "spot"
	suffix := ""
	if market == "futures" {
		path = "futures/um"
		suffix = "-futures"
	}

	months := []string{}
	for y := from.Year(); y <= to.Year(); y++ {
		mStart := 1
		if y == from.Year() {
			mStart = int(from.Month())
		}
		mEnd := 12
		if y == to.Year() {
			mEnd = int(to.Month())
		}
		for m := mStart; m <= mEnd; m++ {
			months = append(months, fmt.Sprintf("%04d-%02d", y, m))
		}
	}

	all := [][]string{}
	header := []string{"openTime", "open", "high", "low", "close", "volume", "closeTime", "quoteVolume", "trades", "takerBuyBase", "takerBuyQuote", "ignore"}
	for _, mon := range months {
		url := fmt.Sprintf("%s/%s/monthly/klines/%s/%s/%s-%s-%s.zip", baseURL, path, symbol, interval, symbol, interval, mon)
		rows, err := fetchMonth(url)
		if err != nil {
			fmt.Printf("[skip] %s: %v\n", mon, err)
			continue
		}
		fmt.Printf("[ok]   %s: %d rows\n", mon, len(rows))
		all = append(all, rows...)
	}

	// dedupe by openTime + sort (the months are fetched in order — still sort)
	sort.Slice(all, func(i, j int) bool {
		return all[i][0] < all[j][0]
	})
	dedup := all[:0]
	var prev string
	for _, r := range all {
		if r[0] == prev {
			continue
		}
		prev = r[0]
		dedup = append(dedup, r)
	}

	out := filepath.Join(outDir, fmt.Sprintf("%s-%s%s.csv", symbol, interval, suffix))
	f, err := os.Create(out)
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write(header)
	for _, r := range dedup {
		w.Write(r)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	first, last := "", ""
	if len(dedup) > 0 {
		first = ts(dedup[0][0])
		last = ts(dedup[len(dedup)-1][0])
	}
	fmt.Printf("WROTE %s: %d rows | %s → %s\n", out, len(dedup), first, last)
}

func fetchMonth(url string) ([][]string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, zf := range zr.File {
		if !strings.HasSuffix(zf.Name, ".csv") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		cr := csv.NewReader(rc)
		rows := [][]string{}
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
			if _, err := strconv.ParseInt(rec[0], 10, 64); err != nil {
				continue // the header row (open_time/...) — the futures monthly CSVs carry it as the first line
			}
			// the monthly ZIPs carry openTime/closeTime in MICROSECONDS
			// (16 digits) — normalize to the millisecond (13) so the CSV is
			// the same unit as the REST klines.
			for _, idx := range []int{0, 6} {
				if len(rec[idx]) > 13 {
					if v, err := strconv.ParseInt(rec[idx][:13], 10, 64); err == nil {
						rec[idx] = strconv.FormatInt(v, 10)
					}
				}
			}
			rows = append(rows, rec)
		}
		return rows, nil
	}
	return nil, fmt.Errorf("no csv in %s", url)
}

func ts(ms string) string {
	var n int64
	fmt.Sscanf(ms, "%d", &n)
	return time.UnixMilli(n).UTC().Format("2006-01-02 15:04")
}

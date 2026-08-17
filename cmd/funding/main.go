// ═══ binance-quant — the funding-rate fetcher ════════════════════════════
// The USDT-margined perpetual funding rates from the public data mirror.
// The funding is charged every 8h (fundingIntervalHours) at the recorded
// rate — the backtest must apply it to open positions (the 2x futures
// design, user 2026-08-17).
//
//	https://data.binance.vision/data/futures/um/monthly/fundingRate/{SYMBOL}/{SYMBOL}-fundingRate-{YYYY-MM}.zip
//	CSV: fundingTime,fundingIntervalHours,fundingRate
//	fundingTime = the UTC ms of the funding timestamp.
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

const fundURL = "https://data.binance.vision/data/futures/um/monthly/fundingRate"

func main() {
	symbol := "BTCUSDT"
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Now().UTC()
	if len(os.Args) > 1 {
		symbol = os.Args[1]
	}
	os.MkdirAll("data", 0o755)
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
	for _, mon := range months {
		url := fmt.Sprintf("%s/%s/%s-fundingRate-%s.zip", fundURL, symbol, symbol, mon)
		rows, err := fetchFunding(url)
		if err != nil {
			fmt.Printf("[skip] %s: %v\n", mon, err)
			continue
		}
		fmt.Printf("[ok]   %s: %d funding timestamps\n", mon, len(rows))
		all = append(all, rows...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i][0] < all[j][0] })
	dedup := all[:0]
	var prev string
	for _, r := range all {
		if r[0] == prev {
			continue
		}
		prev = r[0]
		dedup = append(dedup, r)
	}
	out := filepath.Join("data", fmt.Sprintf("%s-funding.csv", symbol))
	f, err := os.Create(out)
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"fundingTime", "fundingIntervalHours", "fundingRate"})
	for _, r := range dedup {
		w.Write(r)
	}
	w.Flush()
	fmt.Printf("WROTE %s: %d funding rows\n", out, len(dedup))
}

func fetchFunding(url string) ([][]string, error) {
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
			if len(rec) < 3 {
				continue
			}
			if _, err := strconv.ParseInt(rec[0], 10, 64); err != nil {
				continue // the header
			}
			rows = append(rows, rec)
		}
		return rows, nil
	}
	return nil, fmt.Errorf("no csv in %s", url)
}

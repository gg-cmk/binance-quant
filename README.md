# binance-quant

BTCUSDT 무기한 선물(USDT-m) 통계 퀀트 백테스트 파이프라인.

## 현재 상태 (2026-08-17)

**검증 결과: 6중 게이트 통과 전략 없음 — 실전 미진입.**

| 패밀리 | 타임프레임 | 콤보 수 | 결과 |
|---|---|---|---|
| HMM×HAR-RV×EVT-POT (레짐 방향 예측) | 15m / 1h / 4h | 252 | 전부 실패 (MC 0% = 우연보다 나쁨, 순익 음수) |
| EMA 크로스 추세추종 + 변동성 타게팅 | 1h / 4h | 36 | 4h가 MC·기대값·Sharpe 통과했으나 승률≥51%·maxDD<20% 게이트 실패 |

상세 결과는 로컬 `docs/` 에 보관 (GitHub에는 코드+README만 유지).

## 6중 게이트 (실전 진입 조건 — 전부 충족 시에만)

1. 기대값 > 비용 (0.26% 왕복 + 펀딩)
2. Sharpe > 1
3. 최대 낙폭 < 20%
4. OOS 승률 ≥ 51%
5. 몬테카를로 우연확률 × 시행횟수 < 1% (Bonferroni 보정)
6. 순이익 > 0

## 데이터

- 공개 미러 `data.binance.vision` (api.binance.com은 지역 차단 HTTP 451)
- `BTCUSDT-15m-futures.csv` (55,392봉, 2025-01-01 → 2026-07-31)
- `BTCUSDT-1h-futures.csv` (13,848봉) / `BTCUSDT-4h-futures.csv` (3,462봉)
- `BTCUSDT-funding.csv` (8h 펀딩비 1,731행, 평균 0.0036%/8h ≈ 연 4%)

## 구성

```
cmd/fetch    데이터 수집 (월별 ZIP → CSV, ms 정규화)
cmd/funding  펀딩비 수집
cmd/backtest 단일 전략 백테스트 (buyhold-2x sanity, meanrev)
cmd/validate 워크포워드 + MC + 6게이트 검증
cmd/sweep    파라미터 스윕 (HMM 패밀리)
cmd/sweep2   파라미터 스윕 (트렌드 패밀리)
cmd/diag*    진단 도구
quant/       엔진 (candle, cost, funding, HMM, HAR-RV, EVT-POT, 검증)
main.go      웹 대시보드 (데이터 요약)
```

## 웹 대시보드

https://binance-quant-production.up.railway.app — 데이터 현황(기간·봉 수·최신가·펀딩 평균) 표시.
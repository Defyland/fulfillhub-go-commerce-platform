# Native HTTP Benchmark: Create Order

## Context

This result measures the in-process Go HTTP handler with `httptest`. It is useful as a regression signal for request decoding, validation, authorization, service orchestration, and response encoding. It is not a substitute for k6 load testing against a running process.

## Command

```sh
go test -bench=. ./internal/api -run '^$'
```

## Environment

| Field | Value |
| --- | --- |
| Date | 2026-05-28 |
| OS | darwin |
| Architecture | arm64 |
| CPU | Apple M1 Max |
| Go | 1.23.3 |

## Result

| Benchmark | Iterations | ns/op |
| --- | ---: | ---: |
| `BenchmarkCreateOrder-10` | 78985 | 15275 |

## Interpretation

The first executable slice can create and validate an order through the HTTP layer in roughly 16 microseconds per operation in-process. The next performance step is a k6 smoke/load run against `go run ./cmd/fulfillhub-api` so latency percentiles and throughput can be measured over the network path.

# Benchmark Results Status

The repository now includes one empirical native Go benchmark result for the first executable HTTP slice.

Current result:

- [2026-05-28 native HTTP benchmark](../../benchmarks/results/2026-05-28-native-http-benchmark.md)

This is not yet a k6 network load test. The next performance milestone must run k6 against `go run ./cmd/fulfillhub-api` and report p50, p95, p99, throughput, and error rate.

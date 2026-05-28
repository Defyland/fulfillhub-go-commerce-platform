# Benchmark Methodology

## Goal

Benchmarking exists to measure whether FulfillHub can keep checkout orchestration predictable under normal and degraded load, not just to publish peak throughput numbers.

## Tooling

- k6 for request generation
- Prometheus for service-side latency and throughput confirmation
- PostgreSQL statistics views for lock and connection analysis
- RabbitMQ management metrics for queue depth and consumer lag

## Scenarios

### Smoke

- Duration: 1 minute
- Users: 5
- Purpose: validate environment wiring and fail fast on obvious regressions

### Load

- Duration: 15 minutes
- Users: 50 steady
- Purpose: observe steady-state latency and error rate

### Stress

- Duration: 20 minutes
- Users: ramp from 50 to 250
- Purpose: locate saturation points and failure modes

### Spike

- Duration: 10 minutes
- Users: jump from 20 to 200 in 30 seconds
- Purpose: validate rate limiting and queue absorption during bursts

## Dataset assumptions

- 10 merchants
- 5 warehouses
- 2,000 active SKUs
- realistic hot-spot distribution so the top 5 percent of SKUs receive disproportionate traffic

## Measurements to record

- p50, p95, and p99 endpoint latency
- requests per second
- HTTP error rate
- order duplication count
- PostgreSQL lock wait and connection saturation notes
- RabbitMQ queue growth, retry, and DLQ counts
- process memory and CPU notes

## Reporting format

Each benchmark result committed under `benchmarks/results/` should include:

1. scenario name
2. hardware and runtime profile
3. command used
4. top-line metrics table
5. anomalies or bottlenecks observed
6. remediation actions if thresholds were missed

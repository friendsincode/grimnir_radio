# Grimnir Radio Load Testing

This directory contains load testing scripts for Grimnir Radio.

## Prerequisites

### Install k6

**macOS:**
```bash
brew install k6
```

**Linux:**
```bash
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

**Docker:**
```bash
docker pull grafana/k6:latest
```

## Running Tests

### Basic Load Test

```bash
# Test against local instance
k6 run api-load-test.js

# Test against custom URL
k6 run --env BASE_URL=https://radio.example.com api-load-test.js

# With custom credentials
k6 run --env USERNAME=admin --env PASSWORD=secret api-load-test.js
```

### Custom Test Scenarios

**Smoke Test (Quick Validation):**
```bash
k6 run --vus 1 --duration 1m api-load-test.js
```

**Stress Test (Find Breaking Point):**
```bash
k6 run --vus 200 --duration 10m api-load-test.js
```

**Spike Test (Sudden Traffic):**
```bash
k6 run --stage 0s:0,1m:1000,5m:1000,1m:0 api-load-test.js
```

**Soak Test (Long Duration):**
```bash
k6 run --vus 50 --duration 2h api-load-test.js
```

### With Docker

```bash
docker run --rm -i grafana/k6:latest run - <api-load-test.js
```

## Test Scenarios

### 1. API Load Test (api-load-test.js)

Tests typical API usage patterns:
- Health checks
- List stations
- Get station details
- List media
- List smart blocks
- Prometheus metrics

**Load Profile:**
- Ramp up: 0 → 50 users (2 min)
- Steady: 50 users (5 min)
- Ramp up: 50 → 100 users (2 min)
- Steady: 100 users (5 min)
- Ramp down: 100 → 0 (2 min)

**Total Duration:** 16 minutes

**Success Criteria:**
- 95th percentile latency < 500ms
- 99th percentile latency < 1000ms
- Error rate < 1%

## Analyzing Results

### Console Output

k6 provides real-time metrics during execution:

```
     █ Health Check
       ✓ health check is 200
       ✓ health check has status

     checks.........................: 100.00% ✓ 5000      ✗ 0
     data_received..................: 1.2 MB  20 kB/s
     data_sent......................: 450 kB  7.5 kB/s
     http_req_blocked...............: avg=1.23ms  min=0s  med=1ms  max=10ms
     http_req_connecting............: avg=0.95ms  min=0s  med=0s   max=8ms
     http_req_duration..............: avg=45ms    min=5ms med=40ms max=150ms
     http_req_failed................: 0.00%   ✓ 0        ✗ 5000
     http_req_receiving.............: avg=0.5ms   min=0s  med=0s   max=5ms
     http_req_sending...............: avg=0.1ms   min=0s  med=0s   max=2ms
     http_req_tls_handshaking.......: avg=0ms     min=0s  med=0s   max=0ms
     http_req_waiting...............: avg=44ms    min=5ms med=39ms max=148ms
     http_reqs......................: 5000    83.333333/s
     iteration_duration.............: avg=10s     min=9s  med=10s  max=11s
     iterations.....................: 500     8.333333/s
     vus............................: 50      min=50     max=100
     vus_max........................: 100     min=100    max=100
```

### Export Results

**JSON Output:**
```bash
k6 run --out json=results.json api-load-test.js
```

**CSV Output (via jq):**
```bash
k6 run --out json=results.json api-load-test.js
cat results.json | jq -r '"\(.metric),\(.data.value),\(.data.time)"' > results.csv
```

**InfluxDB (Real-time Dashboard):**
```bash
k6 run --out influxdb=http://localhost:8086/k6 api-load-test.js
```

**Cloud (k6 Cloud):**
```bash
k6 cloud api-load-test.js
```

## Interpreting Metrics

### Key Metrics

| Metric | Description | Target |
|--------|-------------|--------|
| `http_req_duration` | Total request time | p95 < 500ms |
| `http_req_waiting` | Time to first byte | p95 < 400ms |
| `http_req_failed` | Failed requests | < 1% |
| `checks` | Assertion pass rate | 100% |
| `iterations` | Completed scenarios | N/A |
| `vus` | Virtual users (concurrent) | As configured |

### Performance Grades

**Excellent:**
- p95 latency < 200ms
- p99 latency < 500ms
- Error rate < 0.1%

**Good:**
- p95 latency < 500ms
- p99 latency < 1000ms
- Error rate < 1%

**Acceptable:**
- p95 latency < 1000ms
- p99 latency < 2000ms
- Error rate < 5%

**Needs Optimization:**
- p95 latency > 1000ms
- Error rate > 5%

## Troubleshooting

### High Latency

**Symptoms:** p95 > 1000ms

**Solutions:**
1. Check database query performance
2. Add missing indexes
3. Enable query caching
4. Scale horizontally (add instances)
5. Optimize slow API endpoints

### High Error Rate

**Symptoms:** http_req_failed > 1%

**Solutions:**
1. Check application logs
2. Verify database connections
3. Check for timeouts
4. Monitor resource usage (CPU, memory)
5. Increase connection pool size

### Connection Errors

**Symptoms:** http_req_connecting timeouts

**Solutions:**
1. Increase file descriptor limits
2. Check network configuration
3. Adjust load balancer settings
4. Reduce concurrent users

## Integration with CI/CD

### GitHub Actions Example

```yaml
name: Load Test

on:
  push:
    branches: [main]

jobs:
  load-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Run k6 load test
        uses: grafana/k6-action@v0.3.0
        with:
          filename: scripts/load-testing/api-load-test.js
        env:
          BASE_URL: https://staging.example.com
          USERNAME: ${{ secrets.TEST_USERNAME }}
          PASSWORD: ${{ secrets.TEST_PASSWORD }}

      - name: Upload results
        uses: actions/upload-artifact@v3
        with:
          name: k6-results
          path: summary.json
```

## Performance Baseline

After optimization, expected results:

| VUs | RPS | p95 Latency | p99 Latency | Error Rate |
|-----|-----|-------------|-------------|------------|
| 10  | 100 | 50ms        | 100ms       | 0%         |
| 50  | 500 | 150ms       | 300ms       | 0%         |
| 100 | 800 | 400ms       | 800ms       | 0.1%       |
| 200 | 1000 | 900ms       | 1500ms      | 1%         |

## Resources

- [k6 Documentation](https://k6.io/docs/)
- [k6 Examples](https://k6.io/docs/examples/)
- [Grafana k6 Cloud](https://grafana.com/products/cloud/k6/)

// Grimnir Radio API Load Test
// Tool: k6 (https://k6.io/)
// Usage: k6 run api-load-test.js

import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const apiDuration = new Trend('api_duration');
const requestCount = new Counter('requests');

// Test configuration
export const options = {
  stages: [
    { duration: '2m', target: 50 },   // Ramp up to 50 users
    { duration: '5m', target: 50 },   // Stay at 50 users
    { duration: '2m', target: 100 },  // Ramp up to 100 users
    { duration: '5m', target: 100 },  // Stay at 100 users
    { duration: '2m', target: 0 },    // Ramp down to 0
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],  // 95% < 500ms, 99% < 1s
    http_req_failed: ['rate<0.01'],                   // Error rate < 1%
    errors: ['rate<0.05'],                            // Custom error rate < 5%
  },
};

// Configuration
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const USERNAME = __ENV.USERNAME || 'admin';
const PASSWORD = __ENV.PASSWORD || 'admin';

// Global token (shared across VUs)
let authToken = null;

export function setup() {
  // Login to get JWT token
  const loginRes = http.post(`${BASE_URL}/api/v1/auth/login`, JSON.stringify({
    username: USERNAME,
    password: PASSWORD,
  }), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(loginRes, {
    'login successful': (r) => r.status === 200,
  });

  if (loginRes.status === 200) {
    const body = JSON.parse(loginRes.body);
    authToken = body.token;
    console.log('Authenticated successfully');
    return { token: authToken };
  } else {
    throw new Error('Authentication failed');
  }
}

export default function (data) {
  const params = {
    headers: {
      'Authorization': `Bearer ${data.token}`,
      'Content-Type': 'application/json',
    },
  };

  // Test 1: Health Check (no auth required)
  group('Health Check', function () {
    const res = http.get(`${BASE_URL}/healthz`);
    check(res, {
      'health check is 200': (r) => r.status === 200,
      'health check has status': (r) => r.json('status') === 'ok',
    });
    errorRate.add(res.status !== 200);
    apiDuration.add(res.timings.duration);
    requestCount.add(1);
  });

  sleep(1);

  // Test 2: List Stations
  group('List Stations', function () {
    const res = http.get(`${BASE_URL}/api/v1/stations`, params);
    check(res, {
      'list stations is 200': (r) => r.status === 200,
      'stations is array': (r) => Array.isArray(r.json()),
    });
    errorRate.add(res.status !== 200);
    apiDuration.add(res.timings.duration);
    requestCount.add(1);
  });

  sleep(1);

  // Test 3: Get Station (if exists)
  group('Get Station', function () {
    const listRes = http.get(`${BASE_URL}/api/v1/stations`, params);
    if (listRes.status === 200) {
      const stations = listRes.json();
      if (stations.length > 0) {
        const stationId = stations[0].id;
        const res = http.get(`${BASE_URL}/api/v1/stations/${stationId}`, params);
        check(res, {
          'get station is 200': (r) => r.status === 200,
          'station has id': (r) => r.json('id') === stationId,
        });
        errorRate.add(res.status !== 200);
        apiDuration.add(res.timings.duration);
        requestCount.add(1);
      }
    }
  });

  sleep(1);

  // Test 4: List Media
  group('List Media', function () {
    const res = http.get(`${BASE_URL}/api/v1/media`, params);
    check(res, {
      'list media is 200': (r) => r.status === 200,
      'media is array': (r) => Array.isArray(r.json()),
    });
    errorRate.add(res.status !== 200);
    apiDuration.add(res.timings.duration);
    requestCount.add(1);
  });

  sleep(1);

  // Test 5: List Smart Blocks
  group('List Smart Blocks', function () {
    const res = http.get(`${BASE_URL}/api/v1/smart-blocks`, params);
    check(res, {
      'list smart blocks is 200': (r) => r.status === 200,
      'smart blocks is array': (r) => Array.isArray(r.json()),
    });
    errorRate.add(res.status !== 200);
    apiDuration.add(res.timings.duration);
    requestCount.add(1);
  });

  sleep(1);

  // Test 6: Metrics Endpoint (no auth)
  group('Prometheus Metrics', function () {
    const res = http.get(`${BASE_URL}/metrics`);
    check(res, {
      'metrics is 200': (r) => r.status === 200,
      'metrics has content': (r) => r.body.length > 0,
    });
    errorRate.add(res.status !== 200);
    requestCount.add(1);
  });

  sleep(2);
}

export function teardown(data) {
  console.log('Load test completed');
}

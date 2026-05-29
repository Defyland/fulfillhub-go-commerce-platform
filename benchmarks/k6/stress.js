import smoke from './smoke.js';

export const options = {
  stages: [
    { duration: '5m', target: 50 },
    { duration: '10m', target: 250 },
    { duration: '5m', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.05'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(90)', 'p(95)', 'p(99)'],
};

export default smoke;

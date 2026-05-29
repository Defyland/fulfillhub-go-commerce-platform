import smoke from './smoke.js';

export const options = {
  stages: [
    { duration: '30s', target: 20 },
    { duration: '30s', target: 200 },
    { duration: '3m', target: 200 },
    { duration: '30s', target: 20 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.05'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(90)', 'p(95)', 'p(99)'],
};

export default smoke;

import smoke, { options as smokeOptions } from './smoke.js';

export const options = {
  ...smokeOptions,
  vus: 50,
  duration: '15m',
};

export default smoke;

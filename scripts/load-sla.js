import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: Number(__ENV.VUS || 10),
  duration: __ENV.DURATION || '20s',
  thresholds: { http_req_failed: ['rate<0.01'], http_req_duration: ['p(95)<250'], checks: ['rate>0.99'] },
};
const base = __ENV.BASE_URL || 'http://localhost:8080';
const scenario = JSON.stringify({
  calendar: { timezone: 'Europe/Istanbul', start_minute: 510, end_minute: 1080, version: 1,
    holidays: [{ date: '2026-10-29', closed_from_minute: 0, label: 'Bayram' }] },
  history: [{ status: 'open', at: '2026-10-23T17:30:00+03:00' }],
  as_of: '2026-10-23T17:30:00+03:00', response_minutes: 60, resolution_minutes: 480,
});
export function setup() {
  const response = http.post(`${base}/auth/login`, JSON.stringify({ email: 'uzman@destekmasa.local', password: __ENV.DEMO_PASSWORD || 'DemoDestek2026!' }), { headers: { 'Content-Type': 'application/json' } });
  if (response.status !== 200 || !response.cookies.destekmasa_session?.length) throw new Error('Demo uzman hesabıyla giriş yapılamadı.');
  return { token: response.cookies.destekmasa_session[0].value };
}
export default function (data) {
  const result = http.post(`${base}/sla/preview`, scenario, {
    headers: { 'Content-Type': 'application/json', Cookie: `destekmasa_session=${data.token}` },
  });
  check(result, {
    'HTTP 200': (response) => response.status === 200,
    'weekend deadline correct': (response) => response.status === 200 && response.json('sla.response.due_at') === '2026-10-26T06:00:00Z',
  });
  sleep(0.05);
}
export function teardown(data) {
  if (data?.token) http.post(`${base}/auth/logout`, null, { headers: { Cookie: `destekmasa_session=${data.token}` } });
}

const { chromium } = require('playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const settings = {};
if (fs.existsSync('.env'))
  for (const line of fs.readFileSync('.env', 'utf8').split('\n')) {
    const m = line.match(/^([A-Z_]+)=(.*)$/);
    if (m) settings[m[1]] = m[2].replace(/^(['"])(.*)\1$/, '$2');
  }
const base =
  process.env.BASE_URL ||
  `http://localhost:${process.env.FRONTEND_PORT || settings.FRONTEND_PORT || 3001}`;
(async () => {
  const browser = await chromium.launch({ headless: true });
  const pages = [];
  const errors = [];
  async function login(name) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    pages.push(page);
    page.on('pageerror', (e) => errors.push(e.message));
    await page.goto(base);
    await page.getByLabel('E-posta', { exact: true }).fill(`${name}@destekmasa.local`);
    await page
      .getByLabel('Parola', { exact: true })
      .fill(process.env.DEMO_PASSWORD || settings.DEMO_PASSWORD || 'DemoDestek2026!');
    await page.getByRole('button', { name: 'Giriş yap', exact: true }).click();
    await page.getByRole('button', { name: 'Bilgi bankası', exact: true }).waitFor();
    return page;
  }
  function reportResponse(page) {
    return page.waitForResponse(
      (r) => r.url().includes('/api/reports?') && r.request().method() === 'GET',
    );
  }
  async function checkCards(page, result) {
    const labels = {
      'Toplam talep': 'total',
      'Aktif talep': 'active',
      'Atanmamış aktif': 'unassigned_active',
      Çözüldü: 'resolved',
      Kapalı: 'closed',
    };
    for (const [label, key] of Object.entries(labels)) {
      const card = page
        .locator('.report-cards > div')
        .filter({ has: page.locator('dt').filter({ hasText: new RegExp(`^${label}$`) }) });
      await card.locator('dd').waitFor();
      assert.equal(await card.locator('dd').innerText(), String(result.summary[key]));
    }
    assert.equal(await page.locator('.report-daily tbody tr').count(), result.daily.length);
    assert.equal(
      await page.locator('.report-breakdown meter').count(),
      result.statuses.length + result.priorities.length + result.categories.length,
    );
  }
  try {
    const employee = await login('calisan');
    assert.equal(await employee.getByRole('button', { name: 'Raporlar', exact: true }).count(), 0);
    assert.equal((await employee.request.get(`${base}/api/reports`)).status(), 403);
    const page = await login('lider');
    const [initial] = await Promise.all([
      reportResponse(page),
      page.getByRole('button', { name: 'Raporlar', exact: true }).click(),
    ]);
    assert.equal(initial.status(), 200);
    let report = await initial.json();
    await checkCards(page, report);
    await page.getByRole('heading', { name: 'Güncel uzman iş yükü', exact: true }).waitFor();
    assert.equal(report.daily.length, 30);
    await page.getByLabel('Rapor kategorisi', { exact: true }).selectOption('network');
    const [filtered] = await Promise.all([
      reportResponse(page),
      page.getByRole('button', { name: 'Raporu göster', exact: true }).click(),
    ]);
    assert.equal(filtered.status(), 200);
    report = await filtered.json();
    assert.equal(report.range.category, 'network');
    await checkCards(page, report);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth),
      false,
    );
    fs.mkdirSync('/tmp/destekmasa-browser-artifacts', { recursive: true });
    await page.screenshot({
      path: '/tmp/destekmasa-browser-artifacts/reports-mobile.png',
      fullPage: true,
    });
    await page.getByLabel('Başlangıç tarihi', { exact: true }).fill('2026-10-25');
    await page.getByLabel('Bitiş tarihi', { exact: true }).fill('2026-10-24');
    await page.getByRole('button', { name: 'Raporu göster', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: 'en fazla 366 gün' }).waitFor();
    await page.getByLabel('Başlangıç tarihi', { exact: true }).fill('1901-01-01');
    await page.getByLabel('Bitiş tarihi', { exact: true }).fill('1901-01-02');
    const [empty] = await Promise.all([
      reportResponse(page),
      page.getByRole('button', { name: 'Raporu göster', exact: true }).click(),
    ]);
    assert.equal(empty.status(), 200);
    report = await empty.json();
    await page
      .getByText('Seçilen tarih ve kategoride talep bulunamadı.', { exact: true })
      .waitFor();
    await checkCards(page, report);
    assert.equal(report.summary.total, 0);
    assert.equal(report.sla.average_response_minutes, null);
    await page.route('**/api/reports?**', (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: { code: 'REPORT_UNAVAILABLE', message: 'Rapor şu an hazırlanamadı.' },
        }),
      }),
    );
    await page.getByRole('button', { name: 'Raporu yenile', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: 'Rapor şu an hazırlanamadı.' }).waitFor();
    assert.equal(await page.locator('.report-cards').count(), 0);
    await page.unroute('**/api/reports?**');
    await page.getByRole('button', { name: 'Raporu yenile', exact: true }).click();
    await page
      .getByText('Seçilen tarih ve kategoride talep bulunamadı.', { exact: true })
      .waitFor();
    await page.route('**/api/reports?**', (route) =>
      route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'UNAUTHENTICATED', message: 'Oturum geçersiz.' } }),
      }),
    );
    await page.getByRole('button', { name: 'Raporu yenile', exact: true }).click();
    await page.getByRole('button', { name: 'Giriş yap', exact: true }).waitFor();
    assert.deepEqual(errors, []);
    console.log(
      'PASS reports roles, live totals, category/date filtering, empty data, mobile layout, error recovery and session expiry',
    );
  } finally {
    for (const page of pages) await page.request.post(`${base}/api/auth/logout`);
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});

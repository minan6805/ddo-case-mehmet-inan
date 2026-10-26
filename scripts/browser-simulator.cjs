const { chromium } = require('playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const {
  baseURL: base,
  password,
  contextOptions,
} = require('./browser-support.cjs');
(async () => {
  const browser = await chromium.launch({ headless: true });
  const pages = [],
    errors = [];
  async function login(name) {
    const page = await browser.newPage({
      ...contextOptions,
      viewport: { width: 1280, height: 900 },
    });
    pages.push(page);
    page.on('pageerror', (e) => errors.push(e.message));
    await page.goto(base);
    await page
      .getByLabel('E-posta', { exact: true })
      .fill(`${name}@destekmasa.local`);
    await page.getByLabel('Parola', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Giriş yap', exact: true }).click();
    await page
      .getByRole('button', { name: 'Bilgi bankası', exact: true })
      .waitFor();
    return page;
  }
  try {
    const employee = await login('calisan');
    assert.equal(
      await employee
        .getByRole('button', { name: 'Simülasyon', exact: true })
        .count(),
      0,
    );
    assert.equal(
      (await employee.request.get(`${base}/api/simulations`)).status(),
      403,
    );
    const page = await login('lider');
    const [initial] = await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/simulations?')),
      page.getByRole('button', { name: 'Simülasyon', exact: true }).click(),
    ]);
    assert.equal(initial.status(), 200);
    const result = await initial.json();
    assert.ok(
      result.items.length > 0,
      'Run make simulate before this browser check',
    );
    const run = result.items[0];
    const card = page.getByRole('article', {
      name: `Çalışma ${run.id}`,
      exact: true,
    });
    await card
      .getByRole('heading', { name: run.config.name, exact: true })
      .waitFor();
    assert.equal(
      await card.locator('progress').getAttribute('max'),
      String(run.total_steps),
    );
    if (process.env.EXPECT_RUNNING === '1') assert.equal(run.status, 'running');
    if (run.status === 'running') {
      const refreshed = await page.waitForResponse((r) =>
        r.url().includes('/api/simulations?'),
      );
      const next = (await refreshed.json()).items.find((r) => r.id === run.id);
      assert.ok(
        next.completed_steps > run.completed_steps ||
          next.status === 'completed',
        'Automatic refresh did not show progress',
      );
    }
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      ),
      false,
    );
    fs.mkdirSync('/tmp/destekmasa-browser-artifacts', { recursive: true });
    await page.screenshot({
      path: '/tmp/destekmasa-browser-artifacts/simulator-mobile.png',
      fullPage: true,
    });
    await page.route('**/api/simulations?**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], next_cursor: '' }),
      }),
    );
    await page
      .getByRole('button', { name: 'Durumu yenile', exact: true })
      .click();
    await page
      .getByText('Henüz simülasyon çalıştırılmadı.', { exact: true })
      .waitFor();
    await page.unroute('**/api/simulations?**');
    const stale = {
      ...run,
      status: 'running',
      updated_at: new Date(Date.now() - 120000).toISOString(),
    };
    await page.route('**/api/simulations?**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [stale], next_cursor: '' }),
      }),
    );
    await page
      .getByRole('button', { name: 'Durumu yenile', exact: true })
      .click();
    await page.getByText('Durum güncellenmiyor', { exact: true }).waitFor();
    await page.unroute('**/api/simulations?**');
    await page.route('**/api/simulations?**', (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: {
            code: 'SIMULATOR_UNAVAILABLE',
            message: 'Simülasyon durumu alınamadı.',
          },
        }),
      }),
    );
    await page
      .getByRole('button', { name: 'Durumu yenile', exact: true })
      .click();
    await page
      .getByRole('alert')
      .filter({ hasText: 'Simülasyon durumu alınamadı.' })
      .waitFor();
    await page.unroute('**/api/simulations?**');
    await page
      .getByRole('button', { name: 'Durumu yenile', exact: true })
      .click();
    await card.waitFor();
    await page.getByRole('alert').waitFor({ state: 'hidden' });
    assert.deepEqual(errors, []);
    console.log(
      'PASS simulator roles, status counters, automatic refresh, mobile layout, empty/stale states and error recovery',
    );
  } finally {
    for (const page of pages)
      await page.request.post(`${base}/api/auth/logout`);
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});

const { chromium } = require('playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const settings = {};
if (fs.existsSync('.env')) for (const line of fs.readFileSync('.env', 'utf8').split('\n')) { const m = line.match(/^([A-Z_]+)=(.*)$/); if (m) settings[m[1]] = m[2].replace(/^(['"])(.*)\1$/, '$2'); }
const base = process.env.BASE_URL || `http://localhost:${process.env.FRONTEND_PORT || settings.FRONTEND_PORT || 3000}`;
(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  const errors = []; page.on('pageerror', (e) => errors.push(e.message));
  const created = [];
  try {
    await page.goto(base);
    await page.getByLabel('E-posta', { exact: true }).fill('varlik@destekmasa.local');
    await page.getByLabel('Parola', { exact: true }).fill(process.env.DEMO_PASSWORD || settings.DEMO_PASSWORD || 'DemoDestek2026!');
    await page.getByRole('button', { name: 'Giriş yap', exact: true }).click();
    await page.getByRole('button', { name: 'Varlıklar', exact: true }).click();
    await page.getByRole('heading', { name: 'Varlıklar ve bağımlılıklar', exact: true }).waitFor();
    const run = Date.now();
    for (const [index, kind] of ['server', 'service', 'application'].entries()) {
      await page.getByRole('button', { name: '+ Yeni varlık', exact: true }).click();
      await page.getByLabel('Varlık kodu', { exact: true }).fill(`UI-${run}-${index}`);
      await page.getByLabel('Varlık adı', { exact: true }).fill(`Tarayıcı varlığı ${index}`);
      await page.getByLabel('Varlık türü', { exact: true }).selectOption(kind);
      const [response] = await Promise.all([page.waitForResponse((r) => r.url().endsWith('/api/assets') && r.request().method() === 'POST'), page.getByRole('button', { name: 'Varlığı oluştur', exact: true }).click()]);
      assert.equal(response.status(), 201); created.push((await response.json()).asset);
      await page.getByRole('heading', { name: `UI-${run}-${index} · Tarayıcı varlığı ${index}`, exact: true }).waitFor();
      await page.getByRole('button', { name: '← Varlıklara dön', exact: true }).click();
    }
    for (let i = 0; i < 3; i++) {
      await page.locator('.ticket-row').filter({ hasText: created[i].code }).click();
      await page.getByLabel('Yeni bağımlı varlık numarası', { exact: true }).fill(String(created[(i + 1) % 3].id));
      await page.getByRole('button', { name: 'Bağımlılık ekle', exact: true }).click();
      await page.getByText('Bağımlılık eklendi.', { exact: true }).waitFor();
      await page.getByRole('button', { name: '← Varlıklara dön', exact: true }).click();
    }
    await page.locator('.ticket-row').filter({ hasText: created[0].code }).click();
    await page.getByRole('button', { name: 'Arıza etkisini hesapla', exact: true }).click();
    await page.getByText('3 varlık etkilenebilir', { exact: true }).waitFor();
    assert.equal(await page.locator('.impact-list > li').count(), 3);
    assert.equal(await page.locator('svg[aria-label="Varlık bağımlılık etki grafı"] g').count(), 3);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
    fs.mkdirSync('/tmp/destekmasa-browser-artifacts', { recursive: true });
    await page.screenshot({ path: '/tmp/destekmasa-browser-artifacts/assets-mobile.png', fullPage: true });
    assert.deepEqual(errors, []);
    console.log('PASS asset creation, owner selection, cyclic graph, impact rendering and mobile layout');
  } finally {
    for (const a of created) {
      for (const b of created) if (a.id !== b.id) assert.equal((await page.request.delete(`${base}/api/assets/${a.id}/dependencies/${b.id}`)).status(), 200);
      const response = await page.request.get(`${base}/api/assets/${a.id}`);
      if (response.status() === 200) { const { asset } = await response.json(); const input = Object.fromEntries(['code', 'name', 'kind', 'state', 'owner_id', 'version'].map((k) => [k, asset[k]])); input.state = 'retired'; assert.equal((await page.request.put(`${base}/api/assets/${a.id}`, { data: input })).status(), 200); }
    }
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exitCode = 1; });

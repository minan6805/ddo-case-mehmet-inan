const { chromium } = require('playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const settings = {};
if (fs.existsSync('.env')) for (const line of fs.readFileSync('.env', 'utf8').split('\n')) { const m = line.match(/^([A-Z_]+)=(.*)$/); if (m) settings[m[1]] = m[2].replace(/^(['"])(.*)\1$/, '$2'); }
const base = process.env.BASE_URL || `http://localhost:${process.env.FRONTEND_PORT || settings.FRONTEND_PORT || 3001}`;
const password = process.env.DEMO_PASSWORD || settings.DEMO_PASSWORD || 'DemoDestek2026!';
async function login(page, name) { await page.goto(base); await page.getByLabel('E-posta', { exact: true }).fill(`${name}@destekmasa.local`); await page.getByLabel('Parola', { exact: true }).fill(password); await page.getByRole('button', { name: 'Giriş yap', exact: true }).click(); await page.getByRole('button', { name: 'Değişiklikler', exact: true }).click(); await page.getByRole('heading', { name: 'Değişiklik yönetimi', exact: true }).waitFor(); }
(async () => {
  const browser = await chromium.launch({ headless: true });
  const owner = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  const cab = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  const errors = []; for (const page of [owner, cab]) page.on('pageerror', (e) => errors.push(e.message));
  const cis = [], plans = [];
  async function act(page, plan, action, expected = 200) { const response = await page.request.post(`${base}/api/changes/${plan.id}/actions`, { headers: { 'Idempotency-Key': crypto.randomUUID() }, data: { action, note: 'Tarayıcı kabul testi', version: plan.version } }); assert.equal(response.status(), expected, await response.text()); return response.json(); }
  try {
    await login(owner, 'varlik');
    const run = Date.now();
    for (let i = 0; i < 3; i++) { const r = await owner.request.post(`${base}/api/assets`, { headers: { 'Idempotency-Key': crypto.randomUUID() }, data: { code: `CAB-UI-${run}-${i}`, name: `CAB tarayıcı varlığı ${i}`, kind: 'server', state: 'active' } }); assert.equal(r.status(), 201); cis.push((await r.json()).asset); }
    for (const [a, b] of [[0, 2], [1, 2], [2, 0]]) assert.equal((await owner.request.put(`${base}/api/assets/${cis[a].id}/dependencies/${cis[b].id}`)).status(), 200);
    // Refresh the asset selector after the fixture was added.
    await owner.getByRole('button', { name: 'Değişiklikleri yenile', exact: true }).click();
    await owner.getByRole('button', { name: '+ Yeni değişiklik', exact: true }).click();
    const title = `[Tarayıcı CAB] Bakım ${run}`;
    await owner.getByLabel('Değişiklik başlığı', { exact: true }).fill(title);
    await owner.getByLabel('Uygulama planı', { exact: true }).fill('Sunucu sürümü güncellenecek.');
    await owner.getByLabel('Geri dönüş planı', { exact: true }).fill('Önceki sunucu sürümü geri yüklenecek.');
    const dates = await owner.evaluate(() => { const local = (d) => new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16); return { start: local(new Date(Date.now() - 60000)), end: local(new Date(Date.now() + 3600000)) }; });
    await owner.getByLabel('Plan başlangıcı', { exact: true }).fill(dates.start);
    await owner.getByLabel('Plan bitişi', { exact: true }).fill(dates.end);
    await owner.locator('.change-roots label').filter({ hasText: cis[0].code }).getByRole('checkbox').check();
    const [creation] = await Promise.all([owner.waitForResponse((r) => r.url().endsWith('/api/changes') && r.request().method() === 'POST'), owner.getByRole('button', { name: 'Değişiklik taslağını kaydet', exact: true }).click()]);
    assert.equal(creation.status(), 201); let first = (await creation.json()).change; plans.push(first.id);
    await owner.getByRole('heading', { name: title, exact: true }).waitFor();
    await owner.locator('.impact-result').first().getByText('2 varlık etkilenebilir', { exact: true }).waitFor();
    assert.equal(await owner.getByRole('button', { name: 'CAB: Onayla', exact: true }).count(), 0);
    await owner.getByLabel('İşlem açıklaması', { exact: true }).fill('CAB değerlendirmesine sunuyorum.');
    const [submission] = await Promise.all([owner.waitForResponse((r) => r.url().endsWith(`/api/changes/${first.id}/actions`) && r.request().method() === 'POST'), owner.getByRole('button', { name: 'CAB onayına gönder', exact: true }).click()]); assert.equal(submission.status(), 200); first = (await submission.json()).change;
    const input = Object.fromEntries(['title', 'description', 'rollback_plan', 'risk', 'starts_at', 'ends_at', 'root_ids'].map((k) => [k, first[k]])); input.title = `[Tarayıcı CAB] Çakışan ${run}`; input.root_ids = [cis[1].id];
    const secondResponse = await owner.request.post(`${base}/api/changes`, { headers: { 'Idempotency-Key': crypto.randomUUID() }, data: input }); assert.equal(secondResponse.status(), 201); let second = (await secondResponse.json()).change; plans.push(second.id); second = (await act(owner, second, 'submit')).change;
    await login(cab, 'lider');
    await cab.locator('.ticket-row').filter({ hasText: title }).click();
    await cab.getByRole('heading', { name: title, exact: true }).waitFor();
    await cab.getByLabel('İşlem açıklaması', { exact: true }).fill('Etki ve geri dönüş planı incelendi.');
    const [approval] = await Promise.all([cab.waitForResponse((r) => r.url().endsWith(`/api/changes/${first.id}/actions`) && r.request().method() === 'POST'), cab.getByRole('button', { name: 'CAB: Onayla', exact: true }).click()]); assert.equal(approval.status(), 200); first = (await approval.json()).change;
    const overlap = await act(cab, second, 'approve', 409); assert.equal(overlap.error.code, 'CHANGE_OVERLAP'); assert.equal(overlap.conflicts[0].id, first.id);
    await owner.getByRole('button', { name: 'Planı yenile', exact: true }).click();
    await owner.getByRole('button', { name: 'Uygulamayı başlat', exact: true }).waitFor();
    await owner.getByLabel('İşlem açıklaması', { exact: true }).fill('Onaylı zaman penceresinde başlıyorum.');
    await owner.getByRole('button', { name: 'Uygulamayı başlat', exact: true }).click();
    await owner.getByRole('button', { name: 'Uygulamayı tamamla', exact: true }).waitFor();
    await owner.getByLabel('İşlem açıklaması', { exact: true }).fill('Uygulama ve kontroller tamamlandı.');
    await owner.getByRole('button', { name: 'Uygulamayı tamamla', exact: true }).click();
    await owner.locator('.badge').filter({ hasText: /^Tamamlandı$/ }).waitFor();
    second = (await act(cab, second, 'approve')).change; await act(owner, second, 'cancel');
    const thirdResponse = await owner.request.post(`${base}/api/changes`, { headers: { 'Idempotency-Key': crypto.randomUUID() }, data: { ...input, title: `[Tarayıcı CAB] Yeni graf ${run}`, root_ids: [cis[0].id] } }); assert.equal(thirdResponse.status(), 201); let third = (await thirdResponse.json()).change; plans.push(third.id); third = (await act(owner, third, 'submit')).change;
    const edit = Object.fromEntries(['code', 'name', 'kind', 'state', 'owner_id', 'version'].map((k) => [k, cis[0][k]])); edit.name += ' güncellendi'; assert.equal((await owner.request.put(`${base}/api/assets/${cis[0].id}`, { data: edit })).status(), 200);
    await cab.getByRole('button', { name: '← Değişikliklere dön', exact: true }).click(); await cab.getByRole('button', { name: 'Değişiklikleri yenile', exact: true }).click();
    await cab.locator('.ticket-row').filter({ hasText: `[Tarayıcı CAB] Yeni graf ${run}` }).click();
    await cab.getByText('Varlık grafı önceki onaydan sonra değişmiş. Uygulamadan önce taslağa alıp yeniden CAB onayı isteyin.', { exact: true }).waitFor();
    await cab.getByLabel('İşlem açıklaması', { exact: true }).fill('Eski grafla onay denemesi.');
    const [stale] = await Promise.all([cab.waitForResponse((r) => r.url().endsWith(`/api/changes/${third.id}/actions`) && r.request().method() === 'POST'), cab.getByRole('button', { name: 'CAB: Onayla', exact: true }).click()]); assert.equal(stale.status(), 409); assert.equal((await stale.json()).error.code, 'STALE_GRAPH');
    await cab.setViewportSize({ width: 390, height: 844 }); assert.equal(await cab.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
    fs.mkdirSync('/tmp/destekmasa-browser-artifacts', { recursive: true }); await cab.screenshot({ path: '/tmp/destekmasa-browser-artifacts/changes-mobile.png', fullPage: true });
    assert.deepEqual(errors, []); console.log('PASS change draft, independent CAB, shared-impact conflict, execution/completion, stale graph and mobile layout');
  } finally {
    for (const id of plans) { const r = await owner.request.get(`${base}/api/changes/${id}`); if (r.status() === 200) { const { change } = await r.json(); if (['draft', 'submitted', 'approved', 'implementing'].includes(change.status)) await act(owner, change, change.status === 'implementing' ? 'fail' : 'cancel'); } }
    for (const a of cis) { for (const b of cis) if (a.id !== b.id) assert.equal((await owner.request.delete(`${base}/api/assets/${a.id}/dependencies/${b.id}`)).status(), 200); const r = await owner.request.get(`${base}/api/assets/${a.id}`); if (r.status() === 200) { const { asset } = await r.json(); const edit = Object.fromEntries(['code', 'name', 'kind', 'state', 'owner_id', 'version'].map((k) => [k, asset[k]])); edit.state = 'retired'; assert.equal((await owner.request.put(`${base}/api/assets/${a.id}`, { data: edit })).status(), 200); } }
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exitCode = 1; });

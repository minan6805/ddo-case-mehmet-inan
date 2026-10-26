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
  const pages = [];
  const errors = [];
  let article, leader;
  const title = `Tarayıcı çözüm makalesi ${Date.now()}`;
  const text =
    'VPN bağlantınızı kontrol edin. <script>window.articleXSS = true</script>';
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
      .click();
    await page
      .getByRole('heading', { name: 'Bilgi bankası', exact: true })
      .waitFor();
    await page.getByRole('button', { name: 'Ara', exact: true }).waitFor();
    return page;
  }
  async function search(page, q) {
    await page.getByLabel('Makale ara', { exact: true }).fill(q);
    await page.getByRole('button', { name: 'Ara', exact: true }).click();
    await page
      .getByText('Makaleler yükleniyor…', { exact: true })
      .waitFor({ state: 'hidden' });
  }
  try {
    const staff = await login('uzman');
    await staff
      .getByRole('button', { name: '+ Yeni makale', exact: true })
      .click();
    await staff.getByLabel('Makale başlığı', { exact: true }).fill(title);
    await staff.getByLabel('Çözüm adımları', { exact: true }).fill(text);
    await staff
      .getByLabel('Makale kategorisi', { exact: true })
      .selectOption('network');
    const [created] = await Promise.all([
      staff.waitForResponse(
        (r) =>
          r.url().endsWith('/api/knowledge') && r.request().method() === 'POST',
      ),
      staff
        .getByRole('button', { name: 'Taslağı oluştur', exact: true })
        .click(),
    ]);
    assert.equal(created.status(), 201);
    article = (await created.json()).article;
    await staff.getByRole('heading', { name: title, exact: true }).waitFor();
    assert.equal(
      await staff.getByLabel('Yayın durumu', { exact: true }).count(),
      0,
    );
    const employee = await login('calisan');
    assert.equal(
      await employee
        .getByRole('button', { name: '+ Yeni makale', exact: true })
        .count(),
      0,
    );
    assert.equal(
      (
        await employee.request.get(`${base}/api/knowledge/${article.id}`)
      ).status(),
      404,
    );
    await search(employee, title);
    await employee
      .getByText('Aramanıza uygun makale bulunamadı.', { exact: true })
      .waitFor();
    leader = await login('lider');
    await search(leader, title);
    await leader.locator('.ticket-row').filter({ hasText: title }).click();
    await leader
      .getByRole('button', { name: 'Makaleyi düzenle', exact: true })
      .click();
    await leader
      .getByLabel('Yayın durumu', { exact: true })
      .selectOption('published');
    await leader
      .getByRole('button', { name: 'Makaleyi kaydet', exact: true })
      .click();
    await leader.getByText('Makale kaydedildi.', { exact: true }).waitFor();
    await leader.getByText('Yayımlanmış', { exact: true }).waitFor();
    await search(employee, title);
    await employee.locator('.ticket-row').filter({ hasText: title }).click();
    await employee.getByRole('heading', { name: title, exact: true }).waitFor();
    assert.equal(
      await employee.locator('.ticket-description').innerText(),
      text,
    );
    assert.equal(await employee.evaluate(() => window.articleXSS), undefined);
    assert.equal(
      await employee
        .getByRole('button', { name: 'Makaleyi düzenle', exact: true })
        .count(),
      0,
    );
    await employee.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await employee.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      ),
      false,
    );
    fs.mkdirSync('/tmp/destekmasa-browser-artifacts', { recursive: true });
    await employee.screenshot({
      path: '/tmp/destekmasa-browser-artifacts/knowledge-mobile.png',
      fullPage: true,
    });
    await leader
      .getByRole('button', { name: 'Makaleyi düzenle', exact: true })
      .click();
    assert.equal(
      await leader.getByLabel('Makale başlığı', { exact: true }).isDisabled(),
      true,
    );
    await leader
      .getByLabel('Yayın durumu', { exact: true })
      .selectOption('draft');
    await leader
      .getByRole('button', { name: 'Makaleyi kaydet', exact: true })
      .click();
    await leader.getByText('Makale kaydedildi.', { exact: true }).waitFor();
    await employee
      .getByRole('button', { name: '← Makalelere dön', exact: true })
      .click();
    await employee
      .getByText('Aramanıza uygun makale bulunamadı.', { exact: true })
      .waitFor();
    assert.deepEqual(errors, []);
    console.log(
      'PASS knowledge draft, employee visibility, leader publication, search, plain text, new review and mobile layout',
    );
  } finally {
    if (article && leader) {
      const response = await leader.request.get(
        `${base}/api/knowledge/${article.id}`,
      );
      if (response.status() === 200) {
        const { article: current } = await response.json();
        const input = Object.fromEntries(
          ['title', 'body', 'category', 'status', 'version'].map((k) => [
            k,
            current[k],
          ]),
        );
        input.status = 'archived';
        assert.equal(
          (
            await leader.request.put(`${base}/api/knowledge/${article.id}`, {
              data: input,
            })
          ).status(),
          200,
        );
      }
    }
    for (const page of pages)
      await page.request.post(`${base}/api/auth/logout`);
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});

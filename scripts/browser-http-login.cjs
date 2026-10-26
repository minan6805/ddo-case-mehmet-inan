// Regression proof for HTTP IP/LAN access, where crypto.randomUUID is absent.
// Requires Playwright/Chromium. Does not create tickets or change demo profiles.
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { baseURL, password } = require('./browser-support.cjs');

(async () => {
  const target = new URL(baseURL);
  // localhost is a secure context even over HTTP; use a browser-only alias.
  const local = ['localhost', '127.0.0.1'].includes(target.hostname);
  if (local) target.hostname = 'destekmasa-http.test';
  const browser = await chromium.launch({
    headless: true,
    args: local
      ? [
          '--host-resolver-rules=MAP destekmasa-http.test 127.0.0.1',
          '--no-proxy-server',
        ]
      : [],
  });
  try {
    for (const colorScheme of ['light', 'dark']) {
      const context = await browser.newContext({
        viewport: { width: 390, height: 844 },
        colorScheme,
      });
      try {
        const page = await context.newPage();
        const errors = [];
        page.on('pageerror', (error) => errors.push(error.message));
        await page.goto(target.href);
        assert.equal(await page.evaluate(() => isSecureContext), false);
        assert.equal(
          await page.evaluate(() => typeof crypto.randomUUID),
          'undefined',
        );
        for (const account of [
          'calisan',
          'admin',
          'lider',
          'uzman',
          'varlik',
          'admin',
        ]) {
          await page
            .getByLabel('E-posta', { exact: true })
            .fill(`${account}@destekmasa.local`);
          await page.getByLabel('Parola', { exact: true }).fill(password);
          await page
            .getByRole('button', { name: 'Giriş yap', exact: true })
            .click();
          await page
            .getByRole('button', { name: '+ Yeni talep', exact: true })
            .waitFor();
          if (['admin', 'lider'].includes(account)) {
            await page.locator('.email-simulator summary').click();
            await page.getByLabel('Message-ID', { exact: true }).waitFor();
            assert.match(
              await page.getByLabel('Message-ID', { exact: true }).inputValue(),
              /^<demo-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}@destekmasa\.local>$/,
            );
          }
          assert.equal(
            await page.evaluate(
              () => document.documentElement.scrollWidth > innerWidth,
            ),
            false,
          );
          await page
            .getByRole('button', { name: 'Çıkış yap', exact: true })
            .click();
          await page
            .getByRole('button', { name: 'Giriş yap', exact: true })
            .waitFor();
        }
        assert.deepEqual(errors, []);
        console.log(
          `PASS HTTP mobile ${colorScheme}: six login/logout transitions, UUID fallback and no blank screen`,
        );
      } finally {
        await context.close();
      }
    }
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exit(1);
});

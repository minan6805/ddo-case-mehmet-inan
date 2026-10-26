// Optional Playwright proof: theme preferences, storage failures and contrast.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { baseURL: base, password, output } = require('./browser-support.cjs');

async function theme(page, expected) {
  await page.waitForFunction(
    (value) => document.documentElement.dataset.theme === value,
    expected,
  );
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    colorScheme: 'dark',
    viewport: { width: 1280, height: 900 },
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  fs.mkdirSync(output, { recursive: true });
  try {
    await page.goto(base);
    await theme(page, 'dark');
    assert.equal(
      await page.getByLabel('Görünüm teması').inputValue(),
      'system',
    );
    await page.screenshot({
      path: path.join(output, 'theme-dark-login.png'),
      fullPage: true,
    });
    await page.getByLabel('Görünüm teması').selectOption('light');
    await page.reload();
    await theme(page, 'light');
    assert.equal(await page.getByLabel('Görünüm teması').inputValue(), 'light');
    await page.screenshot({
      path: path.join(output, 'theme-light-login.png'),
      fullPage: true,
    });
    const other = await context.newPage();
    await other.goto(base);
    await page.getByLabel('Görünüm teması').selectOption('dark');
    await theme(other, 'dark');
    assert.equal(await other.getByLabel('Görünüm teması').inputValue(), 'dark');
    await page.emulateMedia({ colorScheme: 'light' });
    await theme(page, 'dark'); // Manual choice wins over the operating system.
    await page.getByLabel('Görünüm teması').selectOption('system');
    await theme(page, 'light');
    await page.emulateMedia({ colorScheme: 'dark' });
    await theme(page, 'dark');
    await other.close();
    console.log(
      'PASS system theme, manual override, reload persistence and cross-tab synchronization',
    );

    await page
      .getByLabel('E-posta', { exact: true })
      .fill('admin@destekmasa.local');
    await page.getByLabel('Parola', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Giriş yap', exact: true }).click();
    await page.getByRole('navigation', { name: 'Modüller' }).waitFor();
    // axe-core is optional tooling, never shipped to application users.
    const axePath = process.env.AXE_PATH;
    for (const choice of ['dark', 'light']) {
      await page.getByLabel('Görünüm teması').selectOption(choice);
      for (const name of [
        'Talepler',
        'Bilgi bankası',
        'Varlıklar',
        'Değişiklikler',
        'Raporlar',
        'Simülasyon',
      ]) {
        await page.getByRole('button', { name, exact: true }).click();
        await page.waitForLoadState('networkidle');
        if (axePath) {
          await page.addScriptTag({ path: axePath });
          const violations = await page.evaluate(
            async () =>
              (await axe.run(document, { runOnly: ['color-contrast'] }))
                .violations,
          );
          assert.deepEqual(
            violations.map((item) => ({
              id: item.id,
              nodes: item.nodes.map((node) => node.target),
            })),
            [],
            `${choice}: ${name} contrast`,
          );
        }
      }
    }
    await page.getByLabel('Görünüm teması').selectOption('dark');
    await page.getByRole('button', { name: 'Raporlar', exact: true }).click();
    await page.waitForLoadState('networkidle');
    await page.screenshot({
      path: path.join(output, 'theme-dark-reports.png'),
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: path.join(output, 'theme-dark-mobile.png'),
      fullPage: true,
    });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      true,
      'mobile overflow',
    );
    assert.deepEqual(errors, []);
    console.log(
      'PASS light/dark module navigation, optional contrast scan and mobile layout',
    );

    const blocked = await browser.newContext({ colorScheme: 'light' });
    await blocked.addInitScript(() => {
      Object.defineProperty(window, 'localStorage', {
        get() {
          throw new DOMException('Blocked', 'SecurityError');
        },
      });
    });
    const storagePage = await blocked.newPage();
    storagePage.on('pageerror', (error) => errors.push(error.message));
    await storagePage.goto(base);
    await theme(storagePage, 'light');
    await storagePage.getByLabel('Görünüm teması').selectOption('dark');
    await theme(storagePage, 'dark');
    assert.deepEqual(errors, []);
    await blocked.close();
    console.log('PASS theme remains usable when localStorage access is denied');
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

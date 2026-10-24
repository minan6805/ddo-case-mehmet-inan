// Optional browser proof. Requires Playwright; see TESTING.md.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

const settings = {};
if (fs.existsSync('.env')) {
  for (const line of fs.readFileSync('.env', 'utf8').split('\n')) {
    const match = line.trim().match(/^([A-Z_]+)=(.*)$/);
    if (match) settings[match[1]] = match[2].trim().replace(/^(['"])(.*)\1$/, '$2');
  }
}
const baseURL = process.env.BASE_URL || `http://localhost:${process.env.FRONTEND_PORT || settings.FRONTEND_PORT || '3001'}`;
const password = process.env.DEMO_PASSWORD || settings.DEMO_PASSWORD || 'DemoDestek2026!';
const output = process.env.BROWSER_ARTIFACT_DIR || '/tmp/destekmasa-browser-artifacts';
fs.mkdirSync(output, { recursive: true });

async function login(page, email) {
  await page.goto(baseURL);
  await page.getByLabel('E-posta', { exact: true }).fill(email);
  await page.getByLabel('Parola', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Giriş yap', exact: true }).click();
  await page.getByRole('button', { name: '+ Yeni talep', exact: true }).waitFor({ state: 'visible' });
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const errors = [];
  try {
    const employeeContext = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const employee = await employeeContext.newPage();
    employee.on('pageerror', (error) => errors.push(error.message));
    await login(employee, 'calisan@destekmasa.local');
    await employee.getByRole('button', { name: '+ Yeni talep', exact: true }).click();
    const title = `[Tarayıcı doğrulama] Bağlantı ${Date.now()}`;
    await employee.getByLabel('Konu', { exact: true }).fill(title);
    await employee.getByLabel('Sorununuzu açıklayın', { exact: true }).fill('Tarayıcıdan oluşturuldu.\nİkinci satır korunmalı.');
    await employee.getByLabel('Kategori', { exact: true }).selectOption('network');
    await employee.getByLabel('Öncelik', { exact: true }).selectOption('high');
    const [creation] = await Promise.all([
      employee.waitForResponse((response) => response.url().endsWith('/api/tickets') && response.request().method() === 'POST'),
      employee.getByRole('button', { name: 'Talebi oluştur', exact: true }).click(),
    ]);
    assert.equal(creation.status(), 201);
    const created = (await creation.json()).ticket;
    assert.equal(typeof created.assignee_id, 'number');
    assert.ok(created.assignee_name);
    await employee.getByRole('heading', { name: title, exact: true }).waitFor();
    await employee.locator('.sla-grid').waitFor();
    assert.equal(await employee.locator('.sla-timer').count(), 2);
    await employee.screenshot({ path: path.join(output, 'ticket-desktop.png'), fullPage: true });
    await employee.reload();
    await employee.locator('.ticket-row').filter({ hasText: title }).waitFor();
    assert.equal(await employee.getByRole('button', { name: 'Yönetici erişimini kontrol et' }).count(), 0);
    console.log('PASS employee login, ticket creation, detail and session persistence');

    const staffContext = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const staff = await staffContext.newPage();
    staff.on('pageerror', (error) => errors.push(error.message));
    await login(staff, 'uzman@destekmasa.local');
    await staff.locator('.ticket-row').filter({ hasText: title }).click();
    await staff.getByRole('heading', { name: title, exact: true }).waitFor();
    const privateNote = `Özel ekip notu ${Date.now()}`;
    const publicReply = '<script>alert("xss")</script> Yanıt metni';
    await staff.getByLabel('Görünürlük', { exact: true }).selectOption('internal');
    await staff.getByLabel('İç notunuz', { exact: true }).fill(privateNote);
    await staff.getByRole('button', { name: 'İç not ekle', exact: true }).click();
    await staff.locator('.comment-list').getByText(privateNote, { exact: true }).waitFor();
    await staff.getByLabel('Görünürlük', { exact: true }).selectOption('external');
    await staff.getByLabel('Mesajınız', { exact: true }).fill(publicReply);
    await staff.getByRole('button', { name: 'Mesaj gönder', exact: true }).click();
    await staff.locator('.comment-list').getByText(publicReply, { exact: true }).waitFor();
    await employee.locator('.ticket-row').filter({ hasText: title }).click();
    await employee.locator('.comment-list').getByText(publicReply, { exact: true }).waitFor();
    assert.equal(await employee.getByText(privateNote, { exact: true }).count(), 0);
    assert.equal(await employee.getByLabel('Görünürlük', { exact: true }).count(), 0);
    assert.equal(await employee.locator('.comment-list script').count(), 0);
    await employee.getByLabel('Mesajınız', { exact: true }).fill('Çalışandan ek bilgi');
    await employee.getByRole('button', { name: 'Mesaj gönder', exact: true }).click();
    await employee.locator('.comment-list').getByText('Çalışandan ek bilgi', { exact: true }).waitFor();
    await staff.getByRole('button', { name: 'Mesajları yenile', exact: true }).click();
    await staff.locator('.comment-list').getByText('Çalışandan ek bilgi', { exact: true }).waitFor();
    console.log('PASS public conversation, private note isolation and plain-text rendering');
    await staff.getByRole('button', { name: 'İşlemde', exact: true }).click();
    await staff.getByRole('button', { name: 'Çözüldü', exact: true }).waitFor();
    await staff.getByRole('button', { name: 'Yanıt bekleniyor', exact: true }).click();
    await staff.locator('p.sla-paused').waitFor();
    assert.equal(await staff.locator('.sla-timer').count(), 2);
    await staff.getByRole('button', { name: 'İşlemde', exact: true }).click();
    await staff.getByRole('button', { name: 'Çözüldü', exact: true }).waitFor();
    await staff.getByRole('button', { name: 'Çözüldü', exact: true }).click();
    await staff.getByRole('button', { name: 'Kapalı', exact: true }).waitFor();
    assert.equal((await staff.locator('.ticket-tags .badge').innerText()).trim(), 'Çözüldü');
    console.log('PASS specialist queue, SLA acknowledgement, pause/resume and status updates');

    await employee.setViewportSize({ width: 390, height: 844 });
    await employee.reload();
    await employee.locator('.ticket-row').filter({ hasText: title }).click();
    await employee.getByRole('heading', { name: title, exact: true }).waitFor();
    assert.equal((await employee.locator('.ticket-tags .badge').innerText()).trim(), 'Çözüldü');
    assert.equal(await employee.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
    await employee.locator('.sla-grid').waitFor();
    await employee.screenshot({ path: path.join(output, 'ticket-mobile.png'), fullPage: true });
    await employee.getByRole('button', { name: '← Taleplere dön', exact: true }).click();
    await employee.getByLabel('Durum', { exact: true }).selectOption('resolved');
    await employee.locator('.ticket-row').filter({ hasText: title }).waitFor();
    console.log('PASS mobile layout, refreshed ticket status and filtering');

    await employee.getByRole('button', { name: 'Çıkış yap', exact: true }).click();
    await employee.getByRole('heading', { name: 'DestekMasa’ya giriş yapın' }).waitFor();
    await staff.getByRole('button', { name: 'Çıkış yap', exact: true }).click();
    await staff.getByRole('heading', { name: 'DestekMasa’ya giriş yapın' }).waitFor();
    const adminContext = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const admin = await adminContext.newPage();
    admin.on('pageerror', (error) => errors.push(error.message));
    await login(admin, 'admin@destekmasa.local');
    await admin.locator('.email-simulator summary').click();
    const mailTitle = `[Tarayıcı e-posta] Bağlantı ${Date.now()}`;
    await admin.getByLabel('E-posta konusu', { exact: true }).fill(mailTitle);
    await admin.getByLabel('E-posta metni', { exact: true }).fill('Yeni e-posta açıklaması\n\n-- \nDemo İmza');
    await admin.getByLabel('Ekler (isteğe bağlı)', { exact: true }).setInputFiles({ name: 'baglanti.txt', mimeType: 'text/plain', buffer: Buffer.from('Tarayıcı ek kanıtı\n') });
    const [mailResponse] = await Promise.all([
      admin.waitForResponse((r) => r.url().endsWith('/api/email/ingest') && r.request().method() === 'POST'),
      admin.getByRole('button', { name: 'E-postayı işle', exact: true }).click(),
    ]);
    assert.equal(mailResponse.status(), 201);
    const mailReceipt = (await mailResponse.json()).receipt;
    assert.equal(mailReceipt.created_ticket, true);
    await admin.getByText('E-postadan talep açıldı.', { exact: true }).waitFor();
    const [replayResponse] = await Promise.all([
      admin.waitForResponse((r) => r.url().endsWith('/api/email/ingest') && r.request().method() === 'POST'),
      admin.getByRole('button', { name: 'E-postayı işle', exact: true }).click(),
    ]);
    assert.equal(replayResponse.status(), 200);
    assert.equal((await replayResponse.json()).receipt.comment_id, mailReceipt.comment_id);
    await admin.getByText('Bu e-posta daha önce işlendi; tekrar kayıt açılmadı.', { exact: true }).waitFor();
    await admin.getByRole('button', { name: `Talep #${mailReceipt.ticket_id} aç`, exact: true }).click();
    await admin.getByRole('heading', { name: mailTitle, exact: true }).waitFor();
    await admin.locator('.comment-list').getByText('Yeni e-posta açıklaması', { exact: true }).waitFor();
    assert.equal(await admin.locator('.comment-list').getByText('Demo İmza', { exact: true }).count(), 0);
    const downloadEvent = admin.waitForEvent('download');
    await admin.locator('.attachment-list').getByRole('link', { name: 'baglanti.txt', exact: true }).click();
    const download = await downloadEvent;
    assert.equal(download.suggestedFilename(), 'baglanti.txt');
    const downloadedPath = path.join(output, 'email-attachment.txt');
    await download.saveAs(downloadedPath);
    assert.equal(fs.readFileSync(downloadedPath, 'utf8'), 'Tarayıcı ek kanıtı\n');
    await admin.setViewportSize({ width: 390, height: 844 });
    await admin.locator('.email-simulator').scrollIntoViewIfNeeded();
    assert.equal(await admin.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
    await admin.screenshot({ path: path.join(output, 'email-mobile.png'), fullPage: true });
    await admin.setViewportSize({ width: 1280, height: 900 });
    const splitPrivate = `Bölmede gizli kalmalı ${Date.now()}`;
    await admin.getByLabel('Görünürlük', { exact: true }).selectOption('internal');
    await admin.getByLabel('İç notunuz', { exact: true }).fill(splitPrivate);
    await admin.getByRole('button', { name: 'İç not ekle', exact: true }).click();
    await admin.locator('.comment-list').getByText(splitPrivate, { exact: true }).waitFor();
    await admin.locator('.thread-tools summary').click();
    const splitTitle = `[Tarayıcı bölme] ${Date.now()}`;
    await admin.getByLabel('Ayrılacak talebin başlığı', { exact: true }).fill(splitTitle);
    await admin.locator('.split-candidate').filter({ hasText: splitPrivate }).waitFor();
    const choices = admin.locator('.split-candidate input');
    assert.equal(await choices.count(), 2);
    await choices.nth(0).check(); await choices.nth(1).check();
    const [splitResponse] = await Promise.all([
      admin.waitForResponse((r) => r.url().endsWith(`/api/tickets/${mailReceipt.ticket_id}/split`) && r.request().method() === 'POST'),
      admin.getByRole('button', { name: 'Seçilen 2 mesajı yeni talebe ayır', exact: true }).click(),
    ]);
    assert.equal(splitResponse.status(), 200);
    const splitResult = (await splitResponse.json()).result;
    await admin.getByRole('heading', { name: splitTitle, exact: true }).waitFor();
    await admin.locator('.comment-list').getByText(splitPrivate, { exact: true }).waitFor();
    const ownerView = await employeeContext.newPage();
    ownerView.on('pageerror', (error) => errors.push(error.message));
    await login(ownerView, 'calisan@destekmasa.local');
    await ownerView.locator('.ticket-row').filter({ hasText: splitTitle }).click();
    await ownerView.locator('.comment-list').getByText('Yeni e-posta açıklaması', { exact: true }).waitFor();
    assert.equal(await ownerView.getByText(splitPrivate, { exact: true }).count(), 0);
    assert.equal(await ownerView.locator('.attachment-list a').count(), 1);
    assert.equal(await ownerView.locator('.ticket-description').first().innerText(), 'Başka bir talepten ayrılan konu. Ayrıntılar görülebilir mesajlarda yer alır.');
    await ownerView.getByRole('button', { name: 'Çıkış yap', exact: true }).click();
    await ownerView.close();
    await admin.locator('.thread-tools summary').click();
    await admin.getByLabel('Hedef talep numarası', { exact: true }).fill(String(mailReceipt.ticket_id));
    const [mergeResponse] = await Promise.all([
      admin.waitForResponse((r) => r.url().endsWith(`/api/tickets/${splitResult.target_id}/merge`) && r.request().method() === 'POST'),
      admin.getByRole('button', { name: 'Bu talebi hedefle birleştir', exact: true }).click(),
    ]);
    assert.equal(mergeResponse.status(), 200);
    await admin.getByRole('heading', { name: mailTitle, exact: true }).waitFor();
    await admin.locator('.comment-list').getByText(splitPrivate, { exact: true }).waitFor();
    assert.equal(await admin.locator('.attachment-list a').count(), 1);
    const mergedTicket = (await (await admin.request.get(`${baseURL}/api/tickets/${splitResult.target_id}`)).json()).ticket;
    assert.equal(mergedTicket.status, 'closed');
    assert.equal(mergedTicket.merged_into_ticket_id, mailReceipt.ticket_id);
    console.log('PASS split/merge controls, attachment preservation and private note isolation after moving');
    for (const status of ['in_progress', 'resolved', 'closed']) {
      const response = await admin.request.patch(`${baseURL}/api/tickets/${mailReceipt.ticket_id}/status`, { data: { status } });
      assert.equal(response.status(), 200);
    }
    console.log('PASS email simulator, Message-ID replay, signature cleaning, attachment download and mobile layout');
    await admin.getByRole('button', { name: '+ Yeni talep', exact: true }).click();
    const transferTitle = `[Tarayıcı devir] Destek ${Date.now()}`;
    await admin.getByLabel('Konu', { exact: true }).fill(transferTitle);
    await admin.getByLabel('Sorununuzu açıklayın', { exact: true }).fill('İzin ve atama geçmişi kanıtı.');
    const [transferCreation] = await Promise.all([
      admin.waitForResponse((response) => response.url().endsWith('/api/tickets') && response.request().method() === 'POST'),
      admin.getByRole('button', { name: 'Talebi oluştur', exact: true }).click(),
    ]);
    assert.equal(transferCreation.status(), 201);
    const transfer = (await transferCreation.json()).ticket;
    assert.equal(typeof transfer.assignee_id, 'number');
    await admin.locator('.expert-board summary').click();
    const expertCard = admin.locator('.expert-card').filter({ has: admin.getByRole('heading', { name: transfer.assignee_name, exact: true }) });
    await expertCard.getByLabel('İzinde', { exact: true }).waitFor();
    let restoreNeeded = false;
    try {
      await expertCard.getByLabel('İzinde', { exact: true }).check();
      const [leaveUpdate] = await Promise.all([
        admin.waitForResponse((response) => response.url().endsWith(`/api/assignment/experts/${transfer.assignee_id}`) && response.request().method() === 'PUT'),
        expertCard.getByRole('button', { name: 'Uzman ayarlarını kaydet', exact: true }).click(),
      ]);
      assert.equal(leaveUpdate.status(), 200);
      restoreNeeded = true;
      const changedBoard = await leaveUpdate.json();
      assert.equal(changedBoard.experts.find((item) => item.id === transfer.assignee_id).load, 0);
      await admin.getByRole('heading', { name: transferTitle, exact: true }).waitFor();
      await admin.locator('.assignment-timeline').getByText(/Uzman izne ayrıldı/).waitFor();
      const ticketCheck = await admin.request.get(`${baseURL}/api/tickets/${transfer.id}`);
      assert.equal(ticketCheck.status(), 200);
      assert.notEqual((await ticketCheck.json()).ticket.assignee_id, transfer.assignee_id);
      await expertCard.getByLabel('İzinde', { exact: true }).uncheck();
      const [returnUpdate] = await Promise.all([
        admin.waitForResponse((response) => response.url().endsWith(`/api/assignment/experts/${transfer.assignee_id}`) && response.request().method() === 'PUT'),
        expertCard.getByRole('button', { name: 'Uzman ayarlarını kaydet', exact: true }).click(),
      ]);
      assert.equal(returnUpdate.status(), 200);
      restoreNeeded = false;
      await admin.getByRole('button', { name: 'İşlemde', exact: true }).click();
      await admin.getByRole('button', { name: 'Çözüldü', exact: true }).click();
      await admin.getByRole('button', { name: 'Kapalı', exact: true }).click();
      await admin.setViewportSize({ width: 390, height: 844 });
      await expertCard.scrollIntoViewIfNeeded();
      assert.equal(await admin.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
      await admin.screenshot({ path: path.join(output, 'assignment-admin-mobile.png') });
      await admin.setViewportSize({ width: 1280, height: 900 });
      console.log('PASS automatic assignment, leave transfer, immutable history and mobile expert controls');
    } finally {
      if (restoreNeeded) {
        const current = await (await admin.request.get(`${baseURL}/api/assignment/experts`)).json();
        const previous = current.experts.find((item) => item.id === transfer.assignee_id);
        const keys = ['capacity', 'enabled', 'on_leave', 'timezone', 'shift_start_minute', 'shift_end_minute', 'weekdays', 'skills', 'version'];
        const restore = Object.fromEntries(keys.map((key) => [key, previous[key]]));
        restore.on_leave = false;
        const response = await admin.request.put(`${baseURL}/api/assignment/experts/${transfer.assignee_id}`, { data: restore });
        assert.equal(response.status(), 200);
      }
    }
    await admin.locator('.sla-settings summary').click();
    await admin.getByLabel('Mesai başlangıcı', { exact: true }).waitFor();
    assert.equal(await admin.locator('.sla-policy-row').count(), 16);
    const [calendarUpdate] = await Promise.all([
      admin.waitForResponse((response) => response.url().endsWith('/api/sla/calendar') && response.request().method() === 'PUT'),
      admin.getByRole('button', { name: 'Takvimi kaydet ve yeniden hesapla', exact: true }).click(),
    ]);
    assert.equal(calendarUpdate.status(), 200);
    await admin.getByText('Takvim kaydedildi ve talep süreleri yeniden hesaplandı.', { exact: true }).waitFor();
    const policyRow = admin.locator('.sla-policy-row').first();
    const [policyUpdate] = await Promise.all([
      admin.waitForResponse((response) => response.url().includes('/api/sla/policies/') && response.request().method() === 'PUT'),
      policyRow.getByRole('button', { name: 'Kaydet', exact: true }).click(),
    ]);
    assert.equal(policyUpdate.status(), 200);
    await admin.setViewportSize({ width: 390, height: 844 });
    assert.equal(await admin.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
    await admin.screenshot({ path: path.join(output, 'sla-admin-mobile.png'), fullPage: true });
    await admin.getByRole('button', { name: 'Çıkış yap', exact: true }).click();
    await admin.getByRole('heading', { name: 'DestekMasa’ya giriş yapın' }).waitFor();
    console.log('PASS admin calendar/policy saving and mobile settings');
    assert.deepEqual(errors, []);
    console.log(`PASS logout and no browser exceptions; ticket #${created.id}`);
    console.log(`Screenshots: ${output}`);
  } finally {
    await browser.close();
  }
})().catch((error) => { console.error(error); process.exitCode = 1; });

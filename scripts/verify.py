#!/usr/bin/env python3
"""Live functional checks and PostgreSQL proofs for the F/T identifiers."""
import argparse
import http.cookiejar
import json
import os
import sys
import subprocess
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path
from zoneinfo import ZoneInfo


def load_settings():
    # Read literal KEY=value lines; never execute the .env as shell code.
    values = {}
    path = Path('.env')
    if path.exists():
        for raw in path.read_text().splitlines():
            line = raw.strip()
            if not line or line.startswith('#') or '=' not in line:
                continue
            key, value = line.split('=', 1)
            values[key.strip()] = value.strip().strip('"\'')
    return values


settings = load_settings()


def setting(name, fallback):
    return os.environ.get(name, settings.get(name, fallback))


parser = argparse.ArgumentParser()
parser.add_argument('--full', action='store_true')
parser.add_argument('--knowledge', action='store_true')
parser.add_argument('--reports', action='store_true')
parser.add_argument('--simulator', action='store_true')
parser.add_argument('--auth', action='store_true')
parser.add_argument('--tickets', action='store_true')
parser.add_argument('--sla', action='store_true')
parser.add_argument('--assignment', action='store_true')
parser.add_argument('--comments', action='store_true')
parser.add_argument('--email', action='store_true')
parser.add_argument('--threads', action='store_true')
parser.add_argument('--t3', action='store_true')
parser.add_argument('--assets', action='store_true')
parser.add_argument('--changes', action='store_true')
parser.add_argument('--t4', action='store_true')
args = parser.parse_args()
failed = False
base = f"http://127.0.0.1:{setting('FRONTEND_PORT', '3000')}"
api = f"http://127.0.0.1:{setting('BACKEND_PORT', '8080')}"
print(f"{'KİMLİK':<16} {'SONUÇ':<8} AÇIKLAMA")


def check(name, callback):
    global failed
    try:
        callback()
        print(f'{name:<16} PASS     Doğrulandı')
    except Exception as exc:
        failed = True
        print(f'{name:<16} FAIL     {exc}')


def health(url):
    with urllib.request.urlopen(url, timeout=10) as response:
        result = json.load(response)
        if response.status != 200 or result.get('status') != 'ok' or result.get('database') != 'ok':
            raise ValueError('veritabanı hazır değil')


def web():
    with urllib.request.urlopen(base, timeout=10) as response:
        if response.status != 200 or '<div id="root">' not in response.read().decode():
            raise ValueError('arayüz HTML içeriği bulunamadı')


check('HEALTH', lambda: health(api + '/health'))
check('WEB-HEALTH', lambda: health(base + '/api/health'))
check('WEB', web)


def client():
    jar = http.cookiejar.CookieJar()
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar)), jar


def call(opener, path, method='GET', body=None, extra_headers=None):
    headers = {'Origin': base}
    if body is not None:
        headers['Content-Type'] = 'application/json'
    headers.update(extra_headers or {})
    request = urllib.request.Request(base + '/api' + path, method=method, headers=headers,
                                     data=json.dumps(body).encode() if body is not None else None)
    try:
        response = opener.open(request, timeout=10)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def expect(actual, expected, message):
    if actual != expected:
        raise AssertionError(f'{message}: HTTP {actual}, beklenen {expected}')


password = setting('DEMO_PASSWORD', 'DemoDestek2026!')
accounts = [
    ('calisan', 'employee'), ('uzman', 'specialist'), ('uzman2', 'specialist'), ('uzman3', 'specialist'), ('lider', 'team_lead'),
    ('varlik', 'asset_manager'), ('admin', 'admin'),
]


def verify_auth():
    opener, _ = client()
    expect(call(opener, '/auth/me')[0], 401, 'Oturumsuz erişim')
    for email, candidate in [('calisan@destekmasa.local', 'wrong-password'), ('missing@destekmasa.local', password)]:
        expect(call(opener, '/auth/login', 'POST', {'email': email, 'password': candidate})[0], 401, 'Yanlış kimlik bilgileri')
    for name, role in accounts:
        opener, jar = client()
        email = name + '@destekmasa.local'
        body = {'email': email, 'password': password}
        status, result = call(opener, '/auth/login', 'POST', body)
        expect(status, 200, 'Giriş')
        if result['user']['role'] != role or result['user']['email'] != email:
            raise AssertionError('Hesap veya rol eşleşmiyor')
        cookies = list(jar)
        if len(cookies) != 1:
            raise AssertionError('Oturum çerezi bulunamadı')
        old_token = cookies[0].value
        expect(call(opener, '/auth/me')[0], 200, 'Oturum devamı')
        expect(call(opener, '/auth/login', 'POST', body)[0], 200, 'Oturum yenileme')
        replay = urllib.request.build_opener()
        expect(call(replay, '/auth/me', extra_headers={'Cookie': 'destekmasa_session=' + old_token})[0], 401, 'Eski oturum iptali')
        current = list(jar)[0].value
        expect(call(opener, '/auth/logout', 'POST')[0], 204, 'Çıkış')
        expect(call(opener, '/auth/me')[0], 401, 'Çıkış sonrası erişim')
        expect(call(replay, '/auth/me', extra_headers={'Cookie': 'destekmasa_session=' + current})[0], 401, 'Silinen çerezin tekrar kullanımı')
    opener, _ = client()
    expect(call(opener, '/auth/login', 'POST', {'email': 'admin@destekmasa.local', 'password': password}, {'Origin': 'https://attacker.example'})[0], 403, 'Yabancı kaynak')


def verify_rbac():
    opener, _ = client()
    expect(call(opener, '/admin/overview')[0], 401, 'Oturumsuz yönetici erişimi')
    for name, role in accounts:
        opener, _ = client()
        expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Rol testi girişi')
        try:
            expect(call(opener, '/admin/overview')[0], 200 if role == 'admin' else 403, role + ' yetki kontrolü')
        finally:
            call(opener, '/auth/logout', 'POST')



def verify_tickets():
    owner, _ = client()
    other, _ = client()
    staff, _ = client()
    participants = [(owner, 'calisan'), (other, 'varlik'), (staff, 'uzman')]
    try:
        anonymous, _ = client()
        expect(call(anonymous, '/tickets')[0], 401, 'Oturumsuz talep erişimi')
        for opener, name in participants:
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Talep testi girişi')
        status, categories = call(owner, '/ticket-categories')
        expect(status, 200, 'Kategoriler')
        if 'network' not in [item['code'] for item in categories['items']]:
            raise AssertionError('Ağ kategorisi bulunamadı')
        body = {'title': '[Doğrulama] Ağ bağlantısı', 'description': 'Otomatik kabul senaryosu.', 'priority': 'high', 'category': 'network'}
        key = str(uuid.uuid4())
        headers = {'Idempotency-Key': key}
        expect(call(owner, '/tickets', 'POST', body)[0], 400, 'Eksik işlem anahtarı')
        expect(call(owner, '/tickets', 'POST', {**body, 'requester_id': 999}, headers)[0], 400, 'Talep sahibi taklidi')
        status, result = call(owner, '/tickets', 'POST', body, headers)
        expect(status, 201, 'Talep oluşturma')
        ticket_id = result['ticket']['id']
        status, replay = call(owner, '/tickets', 'POST', body, headers)
        expect(status, 201, 'Tekrar istek')
        if replay != result:
            raise AssertionError('Tekrar istek aynı sonucu döndürmedi')
        expect(call(owner, '/tickets', 'POST', {**body, 'title': 'Farklı içerik'}, headers)[0], 409, 'İşlem anahtarı içerik çakışması')
        expect(call(owner, '/tickets', 'POST', body, {'Idempotency-Key': str(uuid.uuid4()), 'Origin': 'https://attacker.example'})[0], 403, 'Yabancı kaynaktan talep')
        expect(call(owner, f'/tickets/{ticket_id}')[0], 200, 'Kendi talebi')
        expect(call(other, f'/tickets/{ticket_id}')[0], 403, 'Başkasının talebi')
        status, other_result = call(other, '/tickets', 'POST', {**body, 'title': '[Doğrulama] Diğer talep sahibi'}, {'Idempotency-Key': str(uuid.uuid4())})
        expect(status, 201, 'Diğer kullanıcının talebi')
        other_id = other_result['ticket']['id']
        status, listing = call(owner, '/tickets?limit=100')
        expect(status, 200, 'Talep listesi')
        if any(item['requester_id'] != result['ticket']['requester_id'] for item in listing['items']):
            raise AssertionError('Listeye başka kullanıcının talebi sızdı')
        if any(item['id'] == other_id for item in listing['items']):
            raise AssertionError('Diğer talep listede göründü')
        status, page = call(staff, '/tickets?limit=1')
        expect(status, 200, 'Destek kuyruğu')
        if len(page['items']) != 1 or not page['next_cursor']:
            raise AssertionError('Sayfalama imleci eksik')
        status, next_page = call(staff, '/tickets?limit=1&before=' + page['next_cursor'])
        expect(status, 200, 'Sonraki sayfa')
        if not next_page['items'] or next_page['items'][0]['id'] >= page['items'][0]['id']:
            raise AssertionError('Sayfalama tekrar veya yanlış sıra üretti')
        expect(call(owner, f'/tickets/{ticket_id}/status', 'PATCH', {'status': 'in_progress'})[0], 403, 'Çalışanın durum değiştirmesi')
        expect(call(staff, f'/tickets/{ticket_id}/status', 'PATCH', {'status': 'closed'})[0], 409, 'Geçersiz durum geçişi')
        for state in ['in_progress', 'waiting_customer', 'in_progress', 'resolved', 'closed']:
            status, changed = call(staff, f'/tickets/{ticket_id}/status', 'PATCH', {'status': state})
            expect(status, 200, 'Durum değişikliği ' + state)
            if changed['ticket']['status'] != state:
                raise AssertionError('Durum kaydedilmedi')
        status, final = call(owner, f'/tickets/{ticket_id}')
        expect(status, 200, 'Çalışanın güncel durumu görmesi')
        if final['ticket']['status'] != 'closed':
            raise AssertionError('Talep kapatılmadı')
    finally:
        for opener, _ in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_sla_lifecycle():
    owner, _ = client()
    staff, _ = client()
    admin, _ = client()
    other, _ = client()
    participants = [(owner, 'calisan'), (staff, 'uzman'), (admin, 'admin'), (other, 'varlik')]
    try:
        for opener, name in participants:
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'SLA girişi')
        expect(call(owner, '/sla/settings')[0], 403, 'Çalışan ayar erişimi')
        expect(call(staff, '/sla/calendar', 'PUT', {})[0], 403, 'Uzman takvim yetkisi')
        status, settings = call(admin, '/sla/settings')
        expect(status, 200, 'SLA ayarları')
        if len(settings['policies']) != 16:
            raise AssertionError('Kategori/öncelik hedefleri eksik')
        # Write the exact same configuration. This tests versioning/recalculation without changing work hours or holidays.
        calendar = settings['calendar']
        status, updated = call(admin, '/sla/calendar', 'PUT', calendar)
        expect(status, 200, 'Takvimi yeniden hesaplama')
        if updated['calendar']['version'] != calendar['version'] + 1:
            raise AssertionError('Takvim sürümü artmadı')
        expect(call(admin, '/sla/calendar', 'PUT', calendar)[0], 409, 'Eski takvim sürümü')
        policy = next(item for item in settings['policies'] if item['category'] == 'general' and item['priority'] == 'normal')
        policy_body = {key: policy[key] for key in ['response_minutes', 'resolution_minutes', 'version']}
        expect(call(admin, '/sla/policies/general/normal', 'PUT', policy_body)[0], 200, 'Hedef kaydetme')
        expect(call(admin, '/sla/policies/general/normal', 'PUT', policy_body)[0], 409, 'Eski hedef sürümü')
        body = {'title': '[SLA doğrulama] Süre akışı', 'description': 'API üzerinden SLA yaşam döngüsü.', 'priority': 'normal', 'category': 'general'}
        status, result = call(owner, '/tickets', 'POST', body, {'Idempotency-Key': str(uuid.uuid4())})
        expect(status, 201, 'SLA talebi')
        ticket_id = result['ticket']['id']
        status, initial = call(owner, f'/tickets/{ticket_id}/sla')
        expect(status, 200, 'Talep SLA bilgisi')
        if initial['sla']['response']['budget_seconds'] != policy['response_minutes'] * 60 or initial['sla']['response']['due_at'] is None:
            raise AssertionError('Hedef veya son tarih eksik')
        expect(call(other, f'/tickets/{ticket_id}/sla')[0], 403, 'Başkasının SLA bilgisi')
        expect(call(staff, f'/tickets/{ticket_id}/status', 'PATCH', {'status': 'in_progress'})[0], 200, 'İlk kabul yanıtı')
        status, acknowledged = call(owner, f'/tickets/{ticket_id}/sla')
        expect(status, 200, 'İlk yanıt kontrolü')
        if acknowledged['sla']['response']['completed_at'] is None:
            raise AssertionError('İlk kabul yanıtı tamamlanmadı')
        expect(call(staff, f'/tickets/{ticket_id}/status', 'PATCH', {'status': 'waiting_customer'})[0], 200, 'SLA duraklatma')
        status, paused = call(owner, f'/tickets/{ticket_id}/sla')
        expect(status, 200, 'Duraklama bilgisi')
        if not paused['sla']['paused'] or paused['sla']['resolution']['due_at'] is not None:
            raise AssertionError('Beklemede SLA durmadı')
        expect(call(staff, f'/tickets/{ticket_id}/status', 'PATCH', {'status': 'in_progress'})[0], 200, 'SLA devamı')
        status, resumed = call(owner, f'/tickets/{ticket_id}/sla')
        expect(status, 200, 'Devam bilgisi')
        if resumed['sla']['paused'] or resumed['sla']['resolution']['due_at'] is None:
            raise AssertionError('Devam sonrası hedef hesaplanmadı')
        for state in ['resolved', 'closed']:
            expect(call(staff, f'/tickets/{ticket_id}/status', 'PATCH', {'status': state})[0], 200, 'SLA bitişi')
        status, completed = call(owner, f'/tickets/{ticket_id}/sla')
        expect(status, 200, 'Tamamlanma bilgisi')
        if completed['sla']['resolution']['completed_at'] is None:
            raise AssertionError('Çözüm sayacı tamamlanmadı')
        status, alerts = call(staff, '/sla/alerts?limit=20')
        expect(status, 200, 'Uyarı kuyruğu')
        if not isinstance(alerts['items'], list):
            raise AssertionError('Uyarı listesi biçimi geçersiz')
    finally:
        for opener, _ in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_sla_engine():
    opener, _ = client()
    expect(call(opener, '/auth/login', 'POST', {'email': 'uzman@destekmasa.local', 'password': password})[0], 200, 'SLA kanıt girişi')
    calendar = {'timezone': 'Europe/Istanbul', 'start_minute': 510, 'end_minute': 1080, 'version': 1, 'holidays': [
        {'date': '2026-10-28', 'closed_from_minute': 780, 'label': 'Arife'},
        {'date': '2026-10-29', 'closed_from_minute': 0, 'label': 'Bayram'},
    ]}
    def preview(history, as_of, response=60, resolution=120, custom_calendar=None):
        status, result = call(opener, '/sla/preview', 'POST', {'calendar': custom_calendar or calendar, 'history': history,
            'as_of': as_of, 'response_minutes': response, 'resolution_minutes': resolution})
        expect(status, 200, 'SLA hesaplama kanıtı')
        return result['sla']
    def event(state, at):
        return {'status': state, 'at': at}
    try:
        friday = [event('open', '2026-10-23T17:30:00+03:00')]
        weekend = preview(friday, '2026-10-23T17:30:00+03:00')
        if weekend['response']['due_at'] != '2026-10-26T06:00:00Z':
            raise AssertionError('Hafta sonu hesabı yanlış')
        holidays = preview([event('open', '2026-10-28T12:30:00+03:00')], '2026-10-28T12:30:00+03:00')
        if holidays['response']['due_at'] != '2026-10-30T06:00:00Z':
            raise AssertionError('Tam/yarım gün tatil hesabı yanlış')
        history = [event('open', '2026-10-26T09:00:00+03:00'), event('in_progress', '2026-10-26T09:10:00+03:00'), event('waiting_customer', '2026-10-26T09:30:00+03:00')]
        paused = preview(history, '2026-10-27T11:00:00+03:00', resolution=60)
        if not paused['paused'] or paused['resolution']['elapsed_seconds'] != 1800:
            raise AssertionError('Bekleme zamanı sayıldı')
        history.append(event('in_progress', '2026-10-27T11:00:00+03:00'))
        resumed = preview(history, '2026-10-27T11:10:00+03:00', resolution=60)
        if resumed['resolution']['elapsed_seconds'] != 2400 or resumed['resolution']['due_at'] != '2026-10-27T08:30:00Z':
            raise AssertionError('Devam sonrası son tarih yanlış')
        initial = [event('open', '2026-10-26T09:00:00+03:00')]
        warning = preview(initial, '2026-10-26T09:48:00+03:00')
        if warning['response']['state'] != 'warning' or warning['response']['percent'] != 80:
            raise AssertionError('%80 uyarısı eksik')
        breach = preview(initial, '2026-10-26T10:30:00+03:00')
        if breach['response']['state'] != 'breached' or breach['response']['due_at'] != '2026-10-26T07:00:00Z':
            raise AssertionError('İhlal veya orijinal hedef yanlış')
        changed_calendar = {**calendar, 'version': 2, 'holidays': calendar['holidays'] + [{'date': '2026-10-26', 'closed_from_minute': 0, 'label': 'Kapanış'}]}
        changed = preview(initial, '2026-10-26T10:00:00+03:00', custom_calendar=changed_calendar)
        if changed['response']['elapsed_seconds'] != 0 or changed['response']['due_at'] != '2026-10-27T06:30:00Z':
            raise AssertionError('Takvim değişikliği yeniden hesaplanmadı')
    finally:
        call(opener, '/auth/logout', 'POST')


def verify_assignment_api():
    owner, _ = client()
    staff, _ = client()
    manager, _ = client()
    participants = [(owner, 'calisan'), (staff, 'uzman'), (manager, 'lider')]
    try:
        for opener, name in participants:
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Atama girişi')
        expect(call(owner, '/assignment/experts')[0], 403, 'Çalışanın uzman panosu')
        status, board = call(manager, '/assignment/experts')
        expect(status, 200, 'Uzman panosu')
        if len(board['experts']) != 3:
            raise AssertionError('Üç demo uzmanı bekleniyordu')
        expert = board['experts'][0]
        fields = ['capacity', 'enabled', 'on_leave', 'timezone', 'shift_start_minute', 'shift_end_minute', 'weekdays', 'skills', 'version']
        profile = {key: expert[key] for key in fields}
        path = f"/assignment/experts/{expert['id']}"
        expect(call(staff, path, 'PUT', profile)[0], 403, 'Uzmanın yönetim yetkisi')
        status, updated = call(manager, path, 'PUT', profile)
        expect(status, 200, 'Liderin uzman kaydı')
        expect(call(manager, path, 'PUT', profile)[0], 409, 'Eski uzman sürümü')
        current = next(item for item in updated['experts'] if item['id'] == expert['id'])
        bad = {key: current[key] for key in fields}
        bad['skills'] = [{'code': 'unknown-skill', 'level': 5}]
        expect(call(manager, path, 'PUT', bad)[0], 400, 'Olmayan beceri')
        body = {'title': '[Atama doğrulama] Otomatik yönlendirme', 'description': 'Beceri, kapasite ve kuyruk API senaryosu.', 'priority': 'normal', 'category': 'general'}
        status, result = call(owner, '/tickets', 'POST', body, {'Idempotency-Key': str(uuid.uuid4())})
        expect(status, 201, 'Otomatik atamalı talep')
        ticket = result['ticket']
        expected_eligible = any(item['enabled'] and not item['on_leave'] and item['on_shift'] and item['load'] < item['capacity'] and any(skill['code'] == 'general' for skill in item['skills']) for item in updated['experts'])
        if expected_eligible and ticket['assignee_id'] is None:
            raise AssertionError('Uygun uzman varken talep atanmadı')
        status, history = call(staff, f"/tickets/{ticket['id']}/assignments")
        expect(status, 200, 'Atama geçmişi')
        if not history['items'] or history['items'][0]['to_expert_id'] != ticket['assignee_id']:
            raise AssertionError('Atama kararı geçmişte bulunamadı')
        expect(call(owner, f"/tickets/{ticket['id']}/assignments")[0], 403, 'Çalışanın dahili geçmişi')
        expect(call(owner, f"/tickets/{ticket['id']}/assignment", 'PATCH')[0], 403, 'Çalışanın atama işlemi')
        expect(call(staff, f"/tickets/{ticket['id']}/assignment", 'PATCH')[0], 200, 'Atamayı tekrar değerlendirme')
        status, after = call(staff, f"/tickets/{ticket['id']}/assignments")
        expect(status, 200, 'Tekrar atama geçmişi')
        if ticket['assignee_id'] is not None and len(after['items']) != len(history['items']):
            raise AssertionError('Uygun atama tekrar denemede gereksiz değişti')
        status, me = call(staff, '/auth/me')
        expect(status, 200, 'Uzman kimliği')
        status, mine = call(staff, '/tickets?queue=mine&limit=100')
        expect(status, 200, 'Bana atananlar')
        if any(item['assignee_id'] != me['user']['id'] for item in mine['items']):
            raise AssertionError('Bana atananlar filtresi başka uzmanı içeriyor')
        expect(call(owner, '/tickets?queue=unassigned')[0], 403, 'Çalışanın ortak kuyruğu')
        status, loads = call(manager, '/assignment/experts')
        expect(status, 200, 'Güncel kapasite')
        if any(item['load'] > item['capacity'] for item in loads['experts']):
            raise AssertionError('Uzman kapasitesi aşıldı')
        for state in ['in_progress', 'resolved', 'closed']:
            expect(call(staff, f"/tickets/{ticket['id']}/status", 'PATCH', {'status': state})[0], 200, 'Test talebini kapat')
    finally:
        for opener, _ in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_assignment_proof():
    # Full verification also proves the other PostgreSQL-backed mechanisms in one isolated run.
    target = 'test-integration' if args.full else 'prove-assignment'
    path = Path('/tmp/destekmasa-assignment-proof.log')
    with path.open('w') as output:
        result = subprocess.run(['make', target], stdout=output, stderr=subprocess.STDOUT, timeout=600)
    text = path.read_text()
    if result.returncode != 0:
        raise AssertionError(f'PostgreSQL kanıtı başarısız; ayrıntı: {path}')
    for scenario in ['two_concurrent_http_requests_cannot_take_one_slot', 'leave_reassigns_all_active_work_with_history', 'resolution_releases_capacity_and_reopening_rechecks_it']:
        if f'--- PASS: TestPostgresAssignmentEngine/{scenario}' not in text:
            raise AssertionError(f'Kanıt senaryosu PASS vermedi: {scenario}; {path}')

def verify_comments():
    participants = []
    try:
        for name in ['calisan', 'uzman', 'varlik']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Mesaj testi girişi')
            participants.append(opener)
        owner, staff, other = participants
        status, result = call(owner, '/tickets', 'POST', {'title': '[Yorum doğrulama] Görünürlük', 'description': 'İç not ve paylaşılan mesaj testi.', 'priority': 'normal', 'category': 'general'}, {'Idempotency-Key': str(uuid.uuid4())})
        expect(status, 201, 'Talep oluştur')
        path = f"/tickets/{result['ticket']['id']}"
        comment_path = path + '/comments'
        public = {'body': 'Paylaşılan yanıt', 'visibility': 'external'}
        private = {'body': 'Yalnızca ekip', 'visibility': 'internal'}
        key = {'Idempotency-Key': str(uuid.uuid4())}
        status, result = call(owner, comment_path, 'POST', public, key)
        expect(status, 201, 'Çalışanın mesajı')
        original = result['comment']['id']
        status, result = call(owner, comment_path, 'POST', public, key)
        expect(status, 201, 'Mesaj tekrar isteği')
        if result['comment']['id'] != original:
            raise AssertionError('Tekrar istek çift mesaj oluşturdu')
        expect(call(owner, comment_path, 'POST', dict(public, body='Farklı'), key)[0], 409, 'Anahtar çakışması')
        expect(call(staff, comment_path, 'POST', private, key)[0], 201, 'İç not')
        expect(call(owner, comment_path, 'POST', private, key)[0], 403, 'Çalışanın iç not yazması')
        for opener in [owner, staff]:
            status, page = call(opener, comment_path)
            expect(status, 200, 'Mesaj listesi')
            if len(page['items']) != (1 if opener is owner else 2):
                raise AssertionError('Mesaj görünürlüğü yanlış')
            if opener is owner and any(c['visibility'] != 'external' for c in page['items']):
                raise AssertionError('İç not çalışana sızdı')
        expect(call(other, comment_path)[0], 403, 'Başkasının mesajını okuma')
        expect(call(other, comment_path, 'POST', public, key)[0], 403, 'Başkasının talebine yazma')
        for state in ['in_progress', 'resolved', 'closed']:
            expect(call(staff, path + '/status', 'PATCH', {'status': state})[0], 200, 'Test talebini kapat')
        expect(call(owner, comment_path, 'POST', public, {'Idempotency-Key': str(uuid.uuid4())})[0], 409, 'Kapalı talebe yeni mesaj')
        expect(call(owner, comment_path, 'POST', public, key)[0], 201, 'Kapanış sonrası başarılı işlemi tekrar al')
    finally:
        for opener in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_email():
    participants = []
    def deliver(opener, raw):
        request = urllib.request.Request(base + '/api/email/ingest', method='POST', headers={'Origin': base, 'Content-Type': 'message/rfc822'}, data=raw)
        try:
            response = opener.open(request, timeout=10)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            return response.status, json.load(response)
    def plain(identifier, sender, subject, body):
        return f"Message-ID: <{identifier}@verify.destekmasa.local>\r\nFrom: {sender}@destekmasa.local\r\nSubject: {subject}\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n{body}".encode()
    try:
        for name in ['lider', 'calisan', 'varlik']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'E-posta testi girişi')
            participants.append(opener)
        manager, owner, other = participants
        identifier = str(uuid.uuid4())
        raw = (f"Message-ID: <{identifier}@verify.destekmasa.local>\r\nFrom: calisan@destekmasa.local\r\nSubject: [E-posta doğrulama] Ek ve yanıt\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYeni açıklama\r\n-- \r\nİmza\r\n--b\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=baglanti.txt\r\nContent-Transfer-Encoding: base64\r\n\r\nVEVTVAo=\r\n--b--\r\n").encode()
        expect(deliver(owner, raw)[0], 403, 'Çalışan e-posta ithalatı')
        status, result = deliver(manager, raw)
        expect(status, 201, 'E-postadan talep aç')
        receipt = result['receipt']
        path = f"/tickets/{receipt['ticket_id']}"
        if not receipt['created_ticket'] or receipt['duplicate']:
            raise AssertionError('İlk e-posta sonucu yanlış')
        status, replay = deliver(manager, raw)
        expect(status, 200, 'Message-ID tekrarını al')
        if not replay['receipt']['duplicate'] or replay['receipt']['comment_id'] != receipt['comment_id']:
            raise AssertionError('E-posta çift mesaj açtı')
        expect(deliver(manager, raw.replace(b'VEVTVAo=', b'RElGRgo='))[0], 409, 'Message-ID farklı içerik')
        status, page = call(owner, path + '/comments')
        expect(status, 200, 'E-posta yorum listesi')
        comment = page['items'][0]
        if comment['body'] != 'Yeni açıklama' or len(comment['attachments']) != 1:
            raise AssertionError('İmza temizleme veya ek kaydı yanlış')
        file = comment['attachments'][0]
        with owner.open(base + f"/api/attachments/{file['id']}", timeout=10) as response:
            if response.read() != b'TEST\n' or response.headers.get('Content-Type') != 'application/octet-stream' or not response.headers.get('Content-Disposition', '').startswith('attachment;'):
                raise AssertionError('Ek indirme yanlış')
        expect(call(other, f"/attachments/{file['id']}")[0], 404, 'Yabancının ek indirmesi')
        reply = plain(identifier + '-reply', 'calisan', f"Re: #{receipt['ticket_id']}", 'Ek bilgi\nOn Monday, Demo wrote:\n> Eski açıklama')
        status, result = deliver(manager, reply)
        expect(status, 201, 'E-posta yanıtı')
        if result['receipt']['created_ticket'] or result['receipt']['ticket_id'] != receipt['ticket_id']:
            raise AssertionError('Yanıt yeni talep oluşturdu')
        expect(deliver(manager, plain(identifier + '-foreign', 'varlik', f"Re: #{receipt['ticket_id']}", 'Yabancı'))[0], 403, 'Gönderenin sahiplik kontrolü')
        status, page = call(owner, path + '/comments')
        expect(status, 200, 'Yanıt listesi')
        if len(page['items']) != 2 or page['items'][0]['body'] != 'Ek bilgi':
            raise AssertionError('Alıntı temizleme veya yanıt sayısı yanlış')
        for state in ['in_progress', 'resolved', 'closed']:
            expect(call(manager, path + '/status', 'PATCH', {'status': state})[0], 200, 'Test talebini kapat')
    finally:
        for opener in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_threads():
    participants = []
    try:
        for name in ['lider', 'calisan', 'varlik']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Düzenleme testi girişi')
            participants.append(opener)
        manager, owner, other = participants
        def create(opener):
            status, result = call(opener, '/tickets', 'POST', {'title': '[Birleştirme doğrulama]', 'description': 'Açık açıklama', 'priority': 'normal', 'category': 'general'}, {'Idempotency-Key': str(uuid.uuid4())})
            expect(status, 201, 'Talep aç')
            return result['ticket']['id']
        source, target, foreign = create(owner), create(owner), create(other)
        notes = []
        for visibility in ['external', 'internal']:
            status, result = call(manager, f'/tickets/{source}/comments', 'POST', {'body': 'Özel not' if visibility == 'internal' else 'Paylaşılan mesaj', 'visibility': visibility}, {'Idempotency-Key': str(uuid.uuid4())})
            expect(status, 201, 'Mesaj ekle')
            notes.append(result['comment']['id'])
        key = {'Idempotency-Key': str(uuid.uuid4())}
        split_body = {'title': '[Bölme doğrulama] Yeni konu', 'comment_ids': notes}
        expect(call(owner, f'/tickets/{source}/split', 'POST', split_body, key)[0], 403, 'Çalışanın bölme yetkisi')
        status, response = call(manager, f'/tickets/{source}/split', 'POST', split_body, key)
        expect(status, 200, 'Talep böl')
        split = response['result']['target_id']
        status, replay = call(manager, f'/tickets/{source}/split', 'POST', split_body, key)
        expect(status, 200, 'Bölme tekrarı')
        if replay['result']['target_id'] != split:
            raise AssertionError('Bölme tekrarı yeni talep açtı')
        status, page = call(owner, f'/tickets/{split}/comments')
        expect(status, 200, 'Bölünmüş mesajlar')
        if len(page['items']) != 1 or page['items'][0]['visibility'] != 'external':
            raise AssertionError('Bölmede iç not sızdı')
        status, detail = call(owner, f'/tickets/{split}')
        expect(status, 200, 'Bölünmüş talep')
        if 'Özel not' in detail['ticket']['description']:
            raise AssertionError('İç not açıklamaya sızdı')
        expect(call(manager, f'/tickets/{split}/merge', 'POST', {'target_id': foreign}, {'Idempotency-Key': str(uuid.uuid4())})[0], 403, 'Farklı sahipleri birleştirme')
        key = {'Idempotency-Key': str(uuid.uuid4())}
        status, result = call(manager, f'/tickets/{split}/merge', 'POST', {'target_id': target}, key)
        expect(status, 200, 'Talep birleştir')
        status, replay = call(manager, f'/tickets/{split}/merge', 'POST', {'target_id': target}, key)
        expect(status, 200, 'Birleştirme tekrarı')
        if result != replay:
            raise AssertionError('Birleştirme tekrar yanıtı değişti')
        status, detail = call(owner, f'/tickets/{split}')
        expect(status, 200, 'Kaynak durumu')
        if detail['ticket']['status'] != 'closed' or detail['ticket']['merged_into_ticket_id'] != target:
            raise AssertionError('Birleşen kaynak kapanmadı')
        status, page = call(owner, f'/tickets/{target}/comments')
        expect(status, 200, 'Hedef mesajları')
        if len(page['items']) != 1 or page['items'][0]['visibility'] != 'external':
            raise AssertionError('Birleştirmede iç not sızdı')
        status, history = call(owner, f'/tickets/{target}/operations')
        expect(status, 200, 'Düzenleme geçmişi')
        if len(history['items']) != 1 or history['items'][0]['kind'] != 'merge':
            raise AssertionError('Birleştirme geçmişi kaydedilmedi')
        expect(call(other, f'/tickets/{target}/operations')[0], 403, 'Yabancının işlem geçmişi')
        for identifier in [source, target, foreign]:
            for state in ['in_progress', 'resolved', 'closed']:
                expect(call(manager, f'/tickets/{identifier}/status', 'PATCH', {'status': state})[0], 200, 'Doğrulama talebini kapat')
    finally:
        for opener in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_t3_proof():
    path = Path('/tmp/destekmasa-assignment-proof.log') if args.full else Path('/tmp/destekmasa-t3-proof.log')
    if not args.full:
        with path.open('w') as output:
            result = subprocess.run(['make', 'prove-t3'], stdout=output, stderr=subprocess.STDOUT, timeout=600)
        if result.returncode != 0:
            raise AssertionError(f'T-3 PostgreSQL kanıtı başarısız: {path}')
    text = path.read_text()
    for scenario in [
        'TestPostgresEmailWorkflow/concurrent_message_id_is_exactly_once',
        'TestPostgresEmailWorkflow/foreign_sender_cannot_guess_ticket_reference',
        'TestPostgresEmailWorkflow/attachment_failure_rolls_back_ticket_routing_sla_and_receipt',
        'TestPostgresThreadWorkflow/merge_moves_comments_and_preserves_privacy_and_origin',
        'TestPostgresThreadWorkflow/split_private_note_never_becomes_public_description',
        'TestPostgresThreadWorkflow/split_then_merge_keeps_attachment_authorization',
        'TestPostgresThreadWorkflow/concurrent_split_cannot_move_comment_twice',
        'TestPostgresThreadWorkflow/failed_merge_rolls_back_close_and_comment_move',
    ]:
        if f'--- PASS: {scenario}' not in text:
            raise AssertionError(f'T-3 senaryosu PASS vermedi: {scenario}; {path}')


def verify_assets():
    participants = []
    created = []
    try:
        for name in ['varlik', 'uzman', 'calisan']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Varlık testi girişi')
            participants.append(opener)
        manager, staff, employee = participants
        expect(call(employee, '/assets')[0], 403, 'Çalışanın envanter erişimi')
        suffix = uuid.uuid4().hex[:10].upper()
        for index, kind in enumerate(['server', 'service', 'application']):
            body = {'code': f'VER-{suffix}-{index}', 'name': '[Doğrulama] Varlık', 'kind': kind, 'state': 'active', 'owner_id': None}
            key = {'Idempotency-Key': str(uuid.uuid4())}
            expect(call(staff, '/assets', 'POST', body, key)[0], 403, 'Uzmanın varlık oluşturması')
            status, result = call(manager, '/assets', 'POST', body, key)
            expect(status, 201, 'Varlık oluştur')
            created.append(result['asset'])
            status, replay = call(manager, '/assets', 'POST', body, key)
            expect(status, 201, 'Varlık tekrar isteği')
            if replay['asset']['id'] != result['asset']['id']:
                raise AssertionError('Tekrar istek çift varlık açtı')
        ids = [a['id'] for a in created]
        for a, b in [(ids[0], ids[1]), (ids[1], ids[2]), (ids[2], ids[0])]:
            expect(call(manager, f'/assets/{a}/dependencies/{b}', 'PUT')[0], 200, 'Graf bağlantısı')
        status, impact = call(staff, f'/assets/{ids[0]}/impact')
        expect(status, 200, 'Döngülü graf etkisi')
        if {a['id'] for a in impact['items']} != set(ids) or len(impact['items']) != 3:
            raise AssertionError('Döngüde yinelenen veya eksik etki')
        status, detail = call(staff, f'/assets/{ids[1]}')
        expect(status, 200, 'Bağımlılık yönü')
        if detail['providers'][0]['id'] != ids[0] or detail['dependents'][0]['id'] != ids[2]:
            raise AssertionError('Sağlayıcı/bağımlı yönü yanlış')
        a = created[0]
        edit = {k: a[k] for k in ['code', 'name', 'kind', 'state', 'owner_id', 'version']}
        edit['name'] = '[Doğrulama] Güncellenmiş varlık'
        expect(call(manager, f"/assets/{a['id']}", 'PUT', edit)[0], 200, 'Varlık düzenle')
        expect(call(manager, f"/assets/{a['id']}", 'PUT', edit)[0], 409, 'Eski sürüm reddi')
    finally:
        if participants:
            manager = participants[0]
            for a in created:
                for b in created:
                    if a['id'] != b['id']:
                        call(manager, f"/assets/{a['id']}/dependencies/{b['id']}", 'DELETE')
                status, result = call(manager, f"/assets/{a['id']}")
                if status == 200:
                    edit = {k: result['asset'][k] for k in ['code', 'name', 'kind', 'state', 'owner_id', 'version']}
                    edit['state'] = 'retired'
                    expect(call(manager, f"/assets/{a['id']}", 'PUT', edit)[0], 200, 'Doğrulama varlığını kullanımdan kaldır')
        for opener in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_changes():
    participants, plans, cis = [], [], []
    try:
        for name in ['varlik', 'lider', 'calisan']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Değişiklik testi girişi')
            participants.append(opener)
        owner, cab, employee = participants
        expect(call(employee, '/changes')[0], 403, 'Çalışanın plan erişimi')
        run = uuid.uuid4().hex[:10].upper()
        for i in range(3):
            status, result = call(owner, '/assets', 'POST', {'code': f'CAB-{run}-{i}', 'name': '[CAB doğrulama] Varlık', 'kind': 'server', 'state': 'active'}, {'Idempotency-Key': str(uuid.uuid4())})
            expect(status, 201, 'CAB varlığı')
            cis.append(result['asset'])
        a, b, c = [v['id'] for v in cis]
        for provider, dependent in [(a, c), (b, c), (c, a)]:
            expect(call(owner, f'/assets/{provider}/dependencies/{dependent}', 'PUT')[0], 200, 'Ortak bağımlılık')
        start = datetime.now(timezone.utc) + timedelta(days=200 + int(run[:2], 16) % 100, minutes=int(run[2:4], 16))
        body = {'title': '[CAB doğrulama] Bakım', 'description': 'Uygulama güncellenecek.', 'rollback_plan': 'Önceki sürüme dönülecek.', 'risk': 'normal', 'starts_at': start.isoformat(), 'ends_at': (start + timedelta(hours=1)).isoformat(), 'root_ids': [a]}
        def create_plan(roots):
            input_body = dict(body, root_ids=roots)
            key = {'Idempotency-Key': str(uuid.uuid4())}
            status, result = call(owner, '/changes', 'POST', input_body, key)
            expect(status, 201, 'Değişiklik taslağı')
            plans.append(result['change']['id'])
            status, replay = call(owner, '/changes', 'POST', input_body, key)
            expect(status, 201, 'Taslak tekrarı')
            if replay['change']['id'] != result['change']['id']:
                raise AssertionError('Tekrar istek ikinci plan açtı')
            return result['change']
        def act(opener, plan, action, expected=200):
            status, result = call(opener, f"/changes/{plan['id']}/actions", 'POST', {'action': action, 'note': 'Otomatik kabul testi', 'version': plan['version']}, {'Idempotency-Key': str(uuid.uuid4())})
            expect(status, expected, action)
            return result['change'] if status == 200 else result
        first, second = create_plan([a]), create_plan([b])
        first = act(owner, first, 'submit')
        second = act(owner, second, 'submit')
        act(owner, first, 'approve', 403)
        first = act(cab, first, 'approve')
        conflict = act(cab, second, 'approve', 409)
        if conflict['error']['code'] != 'CHANGE_OVERLAP' or conflict['conflicts'][0]['id'] != first['id']:
            raise AssertionError('Ortak aşağı varlık çakışması bulunmadı')
        act(owner, first, 'start', 409)  # Future window: execution must not start early.
        first = act(owner, first, 'cancel')
        second = act(cab, second, 'approve')
        second = act(owner, second, 'cancel')
        third = act(owner, create_plan([a]), 'submit')
        asset = cis[0]
        edit = {k: asset[k] for k in ['code', 'name', 'kind', 'state', 'owner_id', 'version']}
        edit['name'] = '[CAB doğrulama] Güncel graf'
        expect(call(owner, f'/assets/{a}', 'PUT', edit)[0], 200, 'Graf sürümünü değiştir')
        stale = act(cab, third, 'approve', 409)
        if stale['error']['code'] != 'STALE_GRAPH':
            raise AssertionError('Eski grafla CAB onayı kabul edildi')
        third = act(owner, third, 'revise')
        third = act(owner, third, 'submit')
        third = act(cab, third, 'approve')
        third = act(owner, third, 'cancel')
        status, history = call(cab, f"/changes/{third['id']}/history?limit=100")
        expect(status, 200, 'CAB geçmişi')
        if not {'create', 'submit', 'revise', 'approve', 'cancel'}.issubset({e['action'] for e in history['items']}):
            raise AssertionError('Karar geçmişi eksik')
    finally:
        if participants:
            owner = participants[0]
            for identifier in plans:
                status, result = call(owner, f'/changes/{identifier}')
                if status == 200:
                    plan = result['change']
                    if plan['status'] in ['draft', 'submitted', 'approved', 'implementing']:
                        action = 'fail' if plan['status'] == 'implementing' else 'cancel'
                        expect(call(owner, f'/changes/{identifier}/actions', 'POST', {'action': action, 'version': plan['version'], 'note': 'Doğrulama sonrası temizlik'}, {'Idempotency-Key': str(uuid.uuid4())})[0], 200, 'Plan temizliği')
            for asset in cis:
                for other in cis:
                    if asset['id'] != other['id']:
                        call(owner, f"/assets/{asset['id']}/dependencies/{other['id']}", 'DELETE')
                status, result = call(owner, f"/assets/{asset['id']}")
                if status == 200:
                    edit = {k: result['asset'][k] for k in ['code', 'name', 'kind', 'state', 'owner_id', 'version']}
                    edit['state'] = 'retired'
                    expect(call(owner, f"/assets/{asset['id']}", 'PUT', edit)[0], 200, 'CAB varlığı temizliği')
        for opener in participants:
            try:
                call(opener, '/auth/logout', 'POST')
            except Exception:
                pass


def verify_simulator():
    worker_log = Path('/tmp/ddo-simulator-worker-proof.log')
    command = ['docker', 'compose', '--profile', 'tools', 'run', '--build', '--rm',
               '-e', 'SIMULATOR_RUN=verify-workload-v1', '-e', 'SIMULATOR_SEED=42',
               '-e', 'SIMULATOR_DAY=2026-10-26', '-e', 'SIMULATOR_COUNT=8',
               '-e', 'SIMULATOR_DAY_SECONDS=0', 'simulator']
    with worker_log.open('w') as output:
        result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, timeout=180)
    if result.returncode != 0:
        raise AssertionError(f'Simülatör servisi başarısız: {worker_log}')
    records = []
    for line in worker_log.read_text().splitlines():
        if line.startswith('{'):
            try:
                records.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    run = next((r for r in reversed(records) if r.get('config', {}).get('name') == 'verify-workload-v1'), None)
    if not run or run['status'] != 'completed' or run['generated_tickets'] != 8 or run['completed_steps'] != run['total_steps']:
        raise AssertionError(f'Simülatör tamamlanma kaydı eksik: {worker_log}')
    participants = []
    try:
        for name in ['lider', 'calisan', 'uzman', 'varlik']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Simülasyon durumu girişi')
            participants.append(opener)
        leader, employee, specialist, asset_manager = participants
        anonymous, _ = client()
        expect(call(anonymous, '/simulations')[0], 401, 'Anonim simülasyon erişimi')
        for opener in [employee, specialist, asset_manager]:
            for path in ['/simulations', f"/simulations/{run['id']}"]:
                expect(call(opener, path)[0], 403, 'Simülasyon rol kontrolü')
        status, detail = call(leader, f"/simulations/{run['id']}")
        expect(status, 200, 'Simülasyon sonucu')
        if detail['run']['generated_tickets'] != 8 or 'checkpoint' in detail['run']:
            raise AssertionError('Simülasyon sayaçları veya gizli checkpoint sınırı hatalı')
        status, page = call(leader, '/simulations?limit=1')
        expect(status, 200, 'Simülasyon sayfalama')
        if len(page['items']) != 1:
            raise AssertionError('Sayfa sınırı uygulanmadı')
    finally:
        for opener in participants:
            call(opener, '/auth/logout', 'POST')
    proof = Path('/tmp/destekmasa-assignment-proof.log') if args.full else Path('/tmp/ddo-simulator-proof.log')
    if not args.full:
        with proof.open('w') as output:
            result = subprocess.run(['make', 'prove-simulator'], stdout=output, stderr=subprocess.STDOUT, timeout=300)
        if result.returncode != 0:
            raise AssertionError(f'Simülatör PostgreSQL kanıtı başarısız: {proof}')
    text = proof.read_text()
    for scenario in ['full_800_ticket_day_and_related_data', 'completed_replay_is_a_database_noop',
                     'unacknowledged_ticket_resumes_without_duplicates', 'failed_shift_restores_and_can_resume',
                     'manual_profile_change_is_not_overwritten', 'concurrent_runner_is_rejected_before_any_writes']:
        if f'--- PASS: TestPostgresSimulator/{scenario}' not in text:
            raise AssertionError(f'Simülatör kanıtı eksik: {scenario}; {proof}')


def verify_reports():
    participants = []
    ticket_id = None
    leader = None
    try:
        for name in ['lider', 'calisan', 'uzman', 'varlik', 'admin']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Rapor girişi')
            participants.append(opener)
        leader, employee, staff, asset_manager, admin = participants
        anonymous, _ = client()
        expect(call(anonymous, '/reports')[0], 401, 'Anonim rapor erişimi')
        for opener in [employee, staff, asset_manager]:
            expect(call(opener, '/reports')[0], 403, 'Yetkisiz rapor erişimi')
        expect(call(admin, '/reports')[0], 200, 'Yönetici raporu')
        for query in ['from=bad&to=2026-10-25', 'from=2026-01-01', 'from=2024-01-01&to=2025-01-01', 'category=missing-category']:
            expect(call(leader, '/reports?' + query)[0], 400, 'Rapor filtresi doğrulaması')
        status, created = call(employee, '/tickets', 'POST', {'title': '[Doğrulama] Rapor verisi', 'description': 'Gerçek API kayıtlarıyla rapor tutarlılığı', 'category': 'software', 'priority': 'critical'}, {'Idempotency-Key': str(uuid.uuid4())})
        expect(status, 201, 'Rapor için talep')
        ticket_id = created['ticket']['id']
        day = datetime.fromisoformat(created['ticket']['created_at'].replace('Z', '+00:00')).astimezone(ZoneInfo('Europe/Istanbul')).date().isoformat()
        status, report = call(leader, '/reports?' + urllib.parse.urlencode({'from': day, 'to': day, 'category': 'software'}))
        expect(status, 200, 'Filtreli rapor')
        tickets_in_range = []
        cursor = ''
        while True:
            status, page = call(leader, '/tickets?limit=100' + ('&before=' + cursor if cursor else ''))
            expect(status, 200, 'Bağımsız talep sayımı')
            for ticket in page['items']:
                created_day = datetime.fromisoformat(ticket['created_at'].replace('Z', '+00:00')).astimezone(ZoneInfo('Europe/Istanbul')).date().isoformat()
                if created_day == day and ticket['category'] == 'software':
                    tickets_in_range.append(ticket)
            cursor = page['next_cursor']
            if not cursor:
                break
        if ticket_id not in {t['id'] for t in tickets_in_range} or report['summary']['total'] != len(tickets_in_range):
            raise AssertionError('Tarih/kategori toplamı gerçek taleplerle eşleşmedi')
        for section, field in [('statuses', 'status'), ('priorities', 'priority'), ('categories', 'category')]:
            for bucket in report[section]:
                if bucket['count'] != sum(t[field] == bucket['code'] for t in tickets_in_range):
                    raise AssertionError('Dağılım gerçek taleplerle eşleşmedi: ' + section)
        if sum(d['count'] for d in report['daily']) != report['summary']['total']:
            raise AssertionError('Günlük toplam tutarsız')
        if report['sla']['tracked'] + report['sla']['missing'] != report['summary']['total']:
            raise AssertionError('SLA örnek sayısı tutarsız')
        if sum(e['active'] for e in report['workload']['experts']) + report['workload']['unassigned_active'] != report['workload']['total_active']:
            raise AssertionError('Global iş yükü tutarsız')
        status, empty = call(leader, '/reports?from=1901-01-01&to=1901-01-02')
        expect(status, 200, 'Boş rapor')
        if empty['summary']['total'] != 0 or len(empty['daily']) != 2 or empty['sla']['average_response_minutes'] is not None:
            raise AssertionError('Boş raporda sıfır/null bekleniyordu')
    finally:
        if ticket_id and leader:
            status, result = call(leader, f'/tickets/{ticket_id}')
            if status == 200:
                current = result['ticket']['status']
                transitions = {'open': ['in_progress', 'resolved', 'closed'], 'in_progress': ['resolved', 'closed'], 'waiting_customer': ['in_progress', 'resolved', 'closed'], 'resolved': ['closed'], 'closed': []}
                for target in transitions[current]:
                    expect(call(leader, f'/tickets/{ticket_id}/status', 'PATCH', {'status': target})[0], 200, 'Rapor talebi temizliği')
        for opener in participants:
            call(opener, '/auth/logout', 'POST')


def verify_knowledge():
    participants = []
    article = None
    leader = None
    try:
        for name in ['uzman', 'lider', 'calisan', 'varlik']:
            opener, _ = client()
            expect(call(opener, '/auth/login', 'POST', {'email': name + '@destekmasa.local', 'password': password})[0], 200, 'Bilgi bankası girişi')
            participants.append(opener)
        staff, leader, employee, manager = participants
        token = uuid.uuid4().hex
        body = {'title': '[Doğrulama] Bilgi bankası ' + token, 'body': 'Aranabilir çözüm ' + token + ' <script>alert(1)</script>', 'category': 'network', 'status': 'draft', 'version': 0}
        key = {'Idempotency-Key': str(uuid.uuid4())}
        expect(call(employee, '/knowledge', 'POST', body, key)[0], 403, 'Çalışan yazamaz')
        expect(call(manager, '/knowledge', 'POST', body, key)[0], 403, 'Varlık yöneticisi yazamaz')
        status, result = call(staff, '/knowledge', 'POST', body, key)
        expect(status, 201, 'Taslak oluşturma')
        article = result['article']
        path = f"/knowledge/{article['id']}"
        status, duplicate = call(staff, '/knowledge', 'POST', body, key)
        expect(status, 201, 'Taslak tekrarı')
        if duplicate['article']['id'] != article['id']:
            raise AssertionError('Çift makale oluştu')
        expect(call(employee, path)[0], 404, 'Taslak gizliliği')
        query = '/knowledge?q=' + token + '&category=network&limit=1'
        status, hidden = call(employee, query)
        expect(status, 200, 'Taslak araması')
        if hidden['items']:
            raise AssertionError('Taslak aramada sızdı')
        edit = {k: article[k] for k in ['title', 'body', 'category', 'status', 'version']}
        edit['status'] = 'published'
        expect(call(staff, path, 'PUT', edit)[0], 403, 'Uzman yayımlayamaz')
        status, result = call(leader, path, 'PUT', edit)
        expect(status, 200, 'Lider yayımlaması')
        article = result['article']
        expect(call(employee, path)[0], 200, 'Yayımlanmış makale')
        status, found = call(employee, query)
        expect(status, 200, 'Yayımlanmış arama')
        if len(found['items']) != 1 or found['items'][0]['id'] != article['id']:
            raise AssertionError('Arama makaleyi bulmadı')
        expect(call(leader, path, 'PUT', edit)[0], 409, 'Eski sürüm reddi')
        edit = {k: article[k] for k in ['title', 'body', 'category', 'status', 'version']}
        edit['body'] = 'Onaylanmamış değişiklik'
        expect(call(leader, path, 'PUT', edit)[0], 400, 'Yayımlanmış içerik korunur')
        edit['body'] = article['body']
        edit['status'] = 'draft'
        status, result = call(leader, path, 'PUT', edit)
        expect(status, 200, 'Yeniden taslak')
        article = result['article']
        expect(call(employee, path)[0], 404, 'Yayından kaldırma')
    finally:
        if article and leader:
            status, result = call(leader, f"/knowledge/{article['id']}")
            if status == 200:
                edit = {k: result['article'][k] for k in ['title', 'body', 'category', 'status', 'version']}
                edit['status'] = 'archived'
                expect(call(leader, f"/knowledge/{article['id']}", 'PUT', edit)[0], 200, 'Makale temizliği')
        for opener in participants:
            call(opener, '/auth/logout', 'POST')


def verify_t4_proof():
    path = Path('/tmp/destekmasa-assignment-proof.log') if args.full else Path('/tmp/ddo-t4-proof.log')
    if not args.full:
        with path.open('w') as output:
            result = subprocess.run(['make', 'prove-t4'], stdout=output, stderr=subprocess.STDOUT, timeout=600)
        if result.returncode != 0:
            raise AssertionError(f'T-4 PostgreSQL kanıtı başarısız: {path}')
    text = path.read_text()
    for scenario in [
        'TestReachableMatchesIndependentFloydWarshall',
        'TestPostgresAssetGraph/cycles_diamonds_and_direction',
        'TestPostgresChangeWorkflow/two_concurrent_http_approvals_only_one_reservation',
        'TestPostgresChangeWorkflow/shared_downstream_asset_conflicts_across_distinct_roots',
        'TestPostgresChangeWorkflow/graph_change_requires_new_review_before_approval',
        'TestPostgresChangeWorkflow/failed_approval_audit_rolls_back_reservation',
    ]:
        if f'--- PASS: {scenario}' not in text:
            raise AssertionError(f'T-4 senaryosu PASS vermedi: {scenario}; {path}')


if args.auth or args.full:
    check('AUTH', verify_auth)
    check('RBAC', verify_rbac)
if args.tickets or args.full:
    check('F-1', verify_tickets)
if args.sla or args.full:
    check('F-2', verify_sla_lifecycle)
    check('T-1', verify_sla_engine)
if args.assignment or args.full:
    check('F-3', verify_assignment_api)
    check('T-2', verify_assignment_proof)
if args.comments or args.full:
    check('COMMENTS', verify_comments)
if args.email or args.full:
    check('EMAIL', verify_email)
if args.threads or args.full:
    check('THREADS', verify_threads)
if args.t3 or args.full:
    check('T-3', verify_t3_proof)
if args.knowledge or args.full:
    check('F-4', verify_knowledge)
if args.assets or args.full:
    check('F-5', verify_assets)
if args.changes or args.full:
    check('F-6', verify_changes)
if args.t4 or args.full:
    check('T-4', verify_t4_proof)
if args.reports or args.full:
    check('F-7', verify_reports)
if args.simulator or args.full:
    check('SIMULATOR', verify_simulator)
sys.exit(1 if failed else 0)

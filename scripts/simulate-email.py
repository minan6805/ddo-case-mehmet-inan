#!/usr/bin/env python3
"""Deterministic, bounded email fixture. It uses only the trusted demo importer.
For the complete daily workload generator, use make simulate.
"""
import email.policy
from email.message import EmailMessage
import http.cookiejar
import json
import os
import random
import re
import time
import urllib.error
import urllib.request


def main():
    base = os.environ.get('API_URL', 'http://api:8080').rstrip('/')
    seed = int(os.environ.get('SIMULATOR_SEED', '42'))
    count = int(os.environ.get('EMAIL_COUNT', '3'))
    run = os.environ.get('SIMULATOR_RUN', 'demo-v1')
    if not 1 <= count <= 50 or not re.fullmatch(r'[a-zA-Z0-9-]{1,40}', run):
        raise ValueError('EMAIL_COUNT 1–50, SIMULATOR_RUN 1–40 ASCII letters/digits/hyphens')
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def request(path, data=None, content_type='application/json'):
        raw = json.dumps(data).encode() if isinstance(data, dict) else data
        for attempt in range(3):
            try:
                req = urllib.request.Request(base + path, data=raw, headers={'Content-Type': content_type})
                with opener.open(req, timeout=10) as response:
                    payload = response.read()
                    return response.status, json.loads(payload) if payload else None
            except urllib.error.HTTPError as exc:
                if exc.code < 500 or attempt == 2:
                    # Never print request headers, passwords, sessions or mail bodies.
                    raise RuntimeError(f'{path}: HTTP {exc.code}') from None
            except (urllib.error.URLError, TimeoutError):
                if attempt == 2:
                    raise RuntimeError(f'{path}: connection failed') from None
            time.sleep(attempt + 1)

    def message(index, subject, body, attachment=False):
        msg = EmailMessage(policy=email.policy.SMTP)
        msg['Message-ID'] = f'<{run}-{seed}-{index}@simulator.destekmasa.local>'
        msg['From'] = 'calisan@destekmasa.local'
        msg['To'] = 'destek@destekmasa.local'
        msg['Subject'] = subject
        msg.set_content(body, charset='utf-8')
        if attachment:
            msg.add_attachment(f'seed={seed}\nitem={index}\n'.encode(), maintype='application', subtype='octet-stream', filename='baglanti.txt')
            msg.set_boundary(f'destekmasa-{run}-{seed}-{index}')
        return msg.as_bytes()

    logged_in = False
    try:
        request('/auth/login', {'email': 'lider@destekmasa.local', 'password': os.environ.get('DEMO_PASSWORD', 'DemoDestek2026!')})
        logged_in = True
        rng = random.Random(seed)
        topics = ['Ağ bağlantısı', 'Uygulama hatası', 'Donanım desteği']
        fresh = duplicates = 0
        for index in range(count):
            raw = message(f'new-{index}', f'[E-posta simülasyonu] {rng.choice(topics)} {index + 1}', 'Yardım rica ediyorum.\n\n-- \nDemo Kullanıcı', attachment=True)
            _, response = request('/email/ingest', raw, 'message/rfc822')
            receipt = response['receipt']
            fresh += not receipt['duplicate']
            duplicates += receipt['duplicate']
            status, replay = request('/email/ingest', raw, 'message/rfc822')
            if status != 200 or not replay['receipt']['duplicate'] or replay['receipt']['comment_id'] != receipt['comment_id']:
                raise RuntimeError('Message-ID deduplication failed')
            duplicates += 1
            reply = message(f'reply-{index}', f"Re: #{receipt['ticket_id']} bilgi", 'Ek bilgi paylaşıyorum.\n\nOn Monday, Demo wrote:\n> Eski açıklama')
            _, response = request('/email/ingest', reply, 'message/rfc822')
            if response['receipt']['created_ticket'] or response['receipt']['ticket_id'] != receipt['ticket_id']:
                raise RuntimeError('Reply routing failed')
            print(json.dumps({'ticket_id': receipt['ticket_id'], 'new_duplicate': receipt['duplicate'], 'reply_duplicate': response['receipt']['duplicate']}, ensure_ascii=False))
        print(json.dumps({'seed': seed, 'run': run, 'new_tickets': fresh, 'deduplicated_deliveries': duplicates, 'reply_deliveries': count}))
    finally:
        if logged_in:
            try:
                request('/auth/logout', b'')
            except Exception:
                pass


if __name__ == '__main__':
    main()

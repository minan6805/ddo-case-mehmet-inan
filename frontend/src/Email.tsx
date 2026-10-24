import { useRef, useState, type FormEvent } from 'react';
import { APIError, request, type User } from './api';

type Receipt = { message_id: string; ticket_id: number; comment_id: number; created_ticket: boolean; duplicate: boolean };
const newID = () => `<demo-${crypto.randomUUID()}@destekmasa.local>`;
const base64 = (data: Uint8Array) => {
  let binary = '';
  for (let i = 0; i < data.length; i += 32768) binary += String.fromCharCode(...data.subarray(i, i + 32768));
  return btoa(binary).match(/.{1,76}/g)?.join('\r\n') ?? '';
};
export function EmailSimulator({ user, onExpired, onOpenTicket }: { user: User; onExpired: () => void; onOpenTicket: (id: number) => void }) {
  const [from, setFrom] = useState('calisan@destekmasa.local');
  const [subject, setSubject] = useState('E-posta ile bağlantı sorunu');
  const [body, setBody] = useState('Bağlantım kesiliyor, yardımcı olabilir misiniz?\n\n-- \nDemo Kullanıcı');
  const [messageID, setMessageID] = useState(newID);
  const [files, setFiles] = useState<File[]>([]);
  const [fileVersion, setFileVersion] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [receipt, setReceipt] = useState<Receipt | null>(null);
  const pending = useRef<{ fingerprint: string; raw: string } | null>(null);
  if (!['admin', 'team_lead'].includes(user.role)) return null;
  async function submit(event: FormEvent) {
    event.preventDefault(); if (busy) return;
    setBusy(true); setError(''); setReceipt(null);
    try {
      if (files.length > 10 || files.some((f) => f.size > 1048576) || files.reduce((n, f) => n + f.size, 0) > 2097152) throw new Error('En fazla 10 ek; tek ek 1 MiB, toplam 2 MiB olabilir.');
      if ([from, subject, messageID].some((s) => /[\r\n]/.test(s))) throw new Error('Başlık alanları tek satır olmalı.');
      const fingerprint = JSON.stringify({ from, subject, body, messageID, fileVersion });
      if (pending.current?.fingerprint !== fingerprint) {
        const boundary = `demo-${crypto.randomUUID()}`;
        const headers = `Message-ID: ${messageID}\r\nFrom: ${from}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\n`;
        let raw = headers + 'Content-Type: text/plain; charset=utf-8\r\n\r\n' + body;
        if (files.length) {
          raw = headers + `Content-Type: multipart/mixed; boundary="${boundary}"\r\n\r\n--${boundary}\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n${body}\r\n`;
          for (const file of files) {
            const filename = file.name.replace(/["\\\r\n]/g, '_');
            raw += `--${boundary}\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename="${filename}"\r\nContent-Transfer-Encoding: base64\r\n\r\n${base64(new Uint8Array(await file.arrayBuffer()))}\r\n`;
          }
          raw += `--${boundary}--\r\n`;
        }
        pending.current = { fingerprint, raw };
      }
      const result = await request<{ receipt: Receipt }>('/email/ingest', { method: 'POST', headers: { 'Content-Type': 'message/rfc822' }, body: pending.current.raw });
      setReceipt(result.receipt);
    } catch (error) {
      if (error instanceof APIError && error.status === 401) { onExpired(); return; }
      setError(error instanceof Error ? error.message : 'E-posta işlenemedi.');
    } finally { setBusy(false); }
  }
  return <section className="status-card email-simulator" aria-labelledby="email-title">
    <details><summary id="email-title">E-posta simülatörü</summary>
      <p className="muted">Lider/admin demo aracı. Kayıtlı bir kullanıcı adına gelen e-postayı taklit eder. Konuda #talepNo varsa o talebe yanıt ekler; gerçek posta göndermez.</p>
      <form onSubmit={submit}>
        <label htmlFor="email-from">Gönderen</label><select id="email-from" value={from} disabled={busy} onChange={(e) => setFrom(e.target.value)}>{['calisan', 'uzman', 'uzman2', 'uzman3', 'lider', 'varlik', 'admin'].map((name) => <option key={name}>{name}@destekmasa.local</option>)}</select>
        <label htmlFor="email-subject">E-posta konusu</label><input id="email-subject" value={subject} disabled={busy} onChange={(e) => setSubject(e.target.value)} maxLength={200} required />
        <label htmlFor="email-id">Message-ID</label><input id="email-id" value={messageID} disabled={busy} onChange={(e) => setMessageID(e.target.value)} maxLength={255} required />
        <p className="muted">Aynı kimlik ve aynı içerik yeniden kayıt oluşturmaz. İçeriği değiştireceksen yeni e-posta kimliği üret.</p>
        <label htmlFor="email-body">E-posta metni</label><textarea id="email-body" value={body} disabled={busy} onChange={(e) => setBody(e.target.value)} maxLength={12000} rows={6} required />
        <label htmlFor="email-files">Ekler (isteğe bağlı)</label><input id="email-files" type="file" multiple disabled={busy} onChange={(e) => { setFiles(Array.from(e.target.files ?? [])); setFileVersion((v) => v + 1); }} />
        <p className="muted">Tek ek 1 MiB; toplam 2 MiB ve en fazla 10 dosya.</p>
        {error && <p role="alert" className="failure">{error}</p>}
        <div className="action-row"><button disabled={busy} type="submit">{busy ? 'İşleniyor…' : 'E-postayı işle'}</button><button disabled={busy} className="secondary" type="button" onClick={() => { setMessageID(newID()); pending.current = null; setReceipt(null); setError(''); }}>Yeni e-posta kimliği</button></div>
      </form>
      {receipt && <div role="status" className="success"><p>{receipt.duplicate ? 'Bu e-posta daha önce işlendi; tekrar kayıt açılmadı.' : receipt.created_ticket ? 'E-postadan talep açıldı.' : 'E-posta talebe yanıt olarak eklendi.'}</p><button className="secondary" onClick={() => onOpenTicket(receipt.ticket_id)}>Talep #{receipt.ticket_id} aç</button></div>}
    </details>
  </section>;
}

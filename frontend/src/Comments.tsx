import { formatDate } from './format';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { APIError, request } from './api';

type Attachment = { id: number; filename: string; size_bytes: number };
type Comment = { attachments: Attachment[]; id: number; author_name: string; body: string; visibility: string; created_at: string };
type Page = { items: Comment[]; next_cursor: string };
export function TicketComments({ id, staff, closed, revision, onExpired, onChanged }: {
  id: number; staff: boolean; closed: boolean; revision: string; onExpired: () => void; onChanged: () => void;
}) {
  const [items, setItems] = useState<Comment[]>([]);
  const [cursor, setCursor] = useState('');
  const [body, setBody] = useState('');
  const [visibility, setVisibility] = useState('external');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [refresh, setRefresh] = useState(0);
  const generation = useRef(0);
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);
  function fail(error: unknown) {
    if (error instanceof APIError && error.status === 401) { onExpired(); return; }
    setError(error instanceof Error ? error.message : 'Mesajlar alınamadı.');
  }
  useEffect(() => {
    let active = true;
    generation.current += 1;
    setLoading(true); setError(''); setItems([]); setCursor('');
    request<Page>(`/tickets/${id}/comments?limit=20`)
      .then((page) => { if (active) { setItems(page.items); setCursor(page.next_cursor); } })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) { onExpired(); return; }
        setError(error instanceof Error ? error.message : 'Mesajlar alınamadı.');
      }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [id, revision, refresh, onExpired]);
  async function older() {
    const current = generation.current;
    setBusy(true); setError('');
    try {
      const page = await request<Page>(`/tickets/${id}/comments?limit=20&before=${encodeURIComponent(cursor)}`);
      if (current === generation.current) { setItems((items) => [...items, ...page.items]); setCursor(page.next_cursor); }
    } catch (error) { if (current === generation.current) fail(error); }
    finally { setBusy(false); }
  }
  async function submit(event: FormEvent) {
    event.preventDefault(); if (busy || !body.trim()) return;
    setBusy(true); setError(''); setNotice('');
    const fingerprint = JSON.stringify({ body: body.trim(), visibility });
    if (pending.current?.fingerprint !== fingerprint) pending.current = { fingerprint, key: crypto.randomUUID() };
    try {
      await request(`/tickets/${id}/comments`, { method: 'POST', headers: { 'Idempotency-Key': pending.current.key }, body: fingerprint });
      setBody(''); pending.current = null;
      setNotice(visibility === 'internal' ? 'İç not kaydedildi.' : 'Mesaj talebe eklendi.');
      setRefresh((value) => value + 1); onChanged();
    } catch (error) { fail(error); }
    finally { setBusy(false); }
  }
  return <section className="ticket-comments" aria-labelledby="comments-title">
    <div className="section-heading"><h3 id="comments-title">Mesajlar ve notlar</h3><button className="secondary" disabled={loading || busy} onClick={() => setRefresh((v) => v + 1)}>Mesajları yenile</button></div>
    {loading && <p role="status">Mesajlar yükleniyor…</p>}
    {error && <p className="failure" role="alert">{error}</p>}
    {notice && <p className="success" role="status">{notice}</p>}
    {!loading && !error && items.length === 0 && <p className="muted">Henüz mesaj yok.</p>}
    <ol className="comment-list">{items.map((item) => <li key={item.id} className={item.visibility === 'internal' ? 'internal-note' : ''}>
      <div className="section-heading"><strong>{item.author_name}</strong><span className="badge">{item.visibility === 'internal' ? 'İç not · yalnızca destek ekibi' : 'Paylaşılan mesaj'}</span></div>
      <time dateTime={item.created_at}>{formatDate(item.created_at)}</time><p className="ticket-description">{item.body}</p>
      {(item.attachments ?? []).length > 0 && <ul className="attachment-list">{item.attachments.map((file) => <li key={file.id}><a href={`/api/attachments/${file.id}`} download>{file.filename}</a> <span className="muted">({file.size_bytes} bayt)</span></li>)}</ul>}
    </li>)}</ol>
    {cursor && <button className="secondary" disabled={busy || loading} onClick={older}>Önceki mesajları göster</button>}
    {!closed && <form onSubmit={submit}>
      {staff && <><label htmlFor="comment-visibility">Görünürlük</label><select id="comment-visibility" value={visibility} disabled={busy} onChange={(event) => setVisibility(event.target.value)}><option value="external">Paylaşılan mesaj — talep eden ve destek ekibi</option><option value="internal">İç not — yalnızca destek ekibi</option></select></>}
      <label htmlFor="comment-body">{visibility === 'internal' ? 'İç notunuz' : 'Mesajınız'}</label>
      <textarea id="comment-body" value={body} disabled={busy} onChange={(event) => setBody(event.target.value)} maxLength={10000} rows={4} required />
      <p className="muted">{visibility === 'internal' ? 'Talep eden bu notu göremez.' : 'Mesajınız talep eden ve destek ekibi tarafından görülür.'}</p>
      <button disabled={busy || !body.trim()} type="submit">{busy ? 'Kaydediliyor…' : visibility === 'internal' ? 'İç not ekle' : 'Mesaj gönder'}</button>
    </form>}
  </section>;
}

import { useEffect, useRef, useState } from 'react';
import { APIError, request } from './api';

type Operation = { id: number; kind: string; source_id: number; target_id: number; actor_name: string; created_at: string };
type Candidate = { id: number; visibility: string; body: string };
type Result = { source_id: number; target_id: number; moved_comments: number };
export function ThreadTools({ id, manager, closed, revision, onExpired, onOpen }: {
  id: number; manager: boolean; closed: boolean; revision: string; onExpired: () => void; onOpen: (id: number) => void;
}) {
  const [history, setHistory] = useState<Operation[]>([]);
  const [cursor, setCursor] = useState('');
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [candidateCursor, setCandidateCursor] = useState('');
  const [selected, setSelected] = useState<number[]>([]);
  const [target, setTarget] = useState('');
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);
  function fail(error: unknown) {
    if (error instanceof APIError && error.status === 401) { onExpired(); return; }
    setError(error instanceof Error ? error.message : 'Talep işlemi tamamlanamadı.');
  }
  useEffect(() => {
    let active = true;
    setLoading(true); setError('');
    Promise.all([
      request<{ items: Operation[]; next_cursor: string }>(`/tickets/${id}/operations?limit=20`),
      manager && !closed ? request<{ items: Candidate[]; next_cursor: string }>(`/tickets/${id}/comments?limit=100`) : Promise.resolve({ items: [], next_cursor: '' }),
    ]).then(([history, comments]) => {
      if (active) { setHistory(history.items); setCursor(history.next_cursor); setCandidates(comments.items); setCandidateCursor(comments.next_cursor); setSelected([]); }
    }).catch((error) => {
      if (!active) return;
      if (error instanceof APIError && error.status === 401) { onExpired(); return; }
      setError(error instanceof Error ? error.message : 'Talep geçmişi alınamadı.');
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [id, manager, closed, revision, attempt, onExpired]);
  async function more(kind: 'history' | 'comments') {
    setBusy(true); setError('');
    try {
      if (kind === 'history') {
        const page = await request<{ items: Operation[]; next_cursor: string }>(`/tickets/${id}/operations?limit=20&before=${cursor}`);
        setHistory((items) => [...items, ...page.items]); setCursor(page.next_cursor);
      } else {
        const page = await request<{ items: Candidate[]; next_cursor: string }>(`/tickets/${id}/comments?limit=100&before=${candidateCursor}`);
        setCandidates((items) => [...items, ...page.items]); setCandidateCursor(page.next_cursor);
      }
    } catch (error) { fail(error); } finally { setBusy(false); }
  }
  async function perform(kind: 'merge' | 'split') {
    if (busy) return; setBusy(true); setError('');
    const body = kind === 'merge' ? { target_id: Number(target) } : { title: title.trim(), comment_ids: selected };
    const fingerprint = JSON.stringify({ kind, body });
    if (pending.current?.fingerprint !== fingerprint) pending.current = { fingerprint, key: crypto.randomUUID() };
    try {
      const result = await request<{ result: Result }>(`/tickets/${id}/${kind}`, { method: 'POST', headers: { 'Idempotency-Key': pending.current.key }, body: JSON.stringify(body) });
      pending.current = null; onOpen(result.result.target_id);
    } catch (error) { fail(error); } finally { setBusy(false); }
  }
  return <section className="thread-tools" aria-labelledby="thread-history-title">
    <h3 id="thread-history-title">Birleştirme ve bölme geçmişi</h3>
    {loading && <p role="status">İşlem geçmişi yükleniyor…</p>}
    {error && <div role="alert"><p className="failure">{error}</p><button className="secondary" disabled={busy} onClick={() => setAttempt((v) => v + 1)}>Geçmişi yenile</button></div>}
    {!loading && history.length === 0 && <p className="muted">Henüz birleştirme/bölme işlemi yok.</p>}
    <ul>{history.map((event) => <li key={event.id}>{event.kind === 'merge' ? 'Birleştirme' : 'Bölme'}: #{event.source_id} → #{event.target_id} · {event.actor_name} <button className="secondary" onClick={() => onOpen(event.source_id === id ? event.target_id : event.source_id)}>İlgili talebi aç</button></li>)}</ul>
    {cursor && <button disabled={busy} className="secondary" onClick={() => more('history')}>Önceki işlemler</button>}
    {manager && !closed && <details><summary>Talebi birleştir veya böl</summary>
      <p className="muted">Yalnızca aynı talep edenin kayıtları birleştirilebilir. Kaynak kapanır; mesajlar ve ekler hedefe geçer, iç notlar gizli kalır. Hedefin SLA süresi korunur. Bu işlemler geri alınmaz.</p>
      <label htmlFor="merge-target">Hedef talep numarası</label><input id="merge-target" type="number" min={1} step={1} value={target} disabled={busy} onChange={(e) => setTarget(e.target.value)} />
      <button disabled={busy || !target || Number(target) === id} onClick={() => perform('merge')}>Bu talebi hedefle birleştir</button>
      <h4>Seçilen mesajlardan yeni talep aç</h4>
      <p className="muted">Yeni başlık talep edene görünür. En fazla 100 mesaj seç; iç notların metni yeni açıklamaya kopyalanmaz. Yeni talebin SLA süresi yeni başlar.</p>
      <label htmlFor="split-title">Ayrılacak talebin başlığı</label><input id="split-title" value={title} disabled={busy} onChange={(e) => setTitle(e.target.value)} maxLength={200} />
      <fieldset disabled={busy || loading}><legend>Ayrılacak mesajlar</legend>{candidates.length === 0 && <p className="muted">Ayrılabilecek mesaj yok.</p>}{candidates.map((c) => <label className="checkbox-label split-candidate" key={c.id}><input type="checkbox" checked={selected.includes(c.id)} onChange={(e) => setSelected((ids) => e.target.checked ? [...ids, c.id] : ids.filter((id) => id !== c.id))} /><span>{c.visibility === 'internal' ? 'İç not' : 'Paylaşılan mesaj'} · {c.body.slice(0, 120)}</span></label>)}</fieldset>
      {candidateCursor && <button disabled={busy} className="secondary" onClick={() => more('comments')}>Daha eski mesajları seç</button>}
      <button disabled={busy || !title.trim() || selected.length < 1 || selected.length > 100} onClick={() => perform('split')}>Seçilen {selected.length} mesajı yeni talebe ayır</button>
    </details>}
  </section>;
}

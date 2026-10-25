import { useEffect, useRef, useState, type FormEvent } from 'react';
import { APIError, request, type User } from './api';
import { formatDate } from './format';

type Article = {
  id: number;
  title: string;
  body: string;
  category: string;
  status: string;
  author_id: number;
  author_name: string;
  version: number;
  updated_at: string;
};
type Category = { code: string; label: string };
type Page = { items: Article[]; next_cursor: string };
const statuses: Record<string, string> = {
  draft: 'Taslak',
  published: 'Yayımlanmış',
  archived: 'Arşivlenmiş',
};

export function Knowledge({ user, onExpired }: { user: User; onExpired: () => void }) {
  const writer = ['specialist', 'team_lead', 'admin'].includes(user.role);
  const reviewer = ['team_lead', 'admin'].includes(user.role);
  const [categories, setCategories] = useState<Category[]>([]);
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('');
  const [status, setStatus] = useState(writer ? '' : 'published');
  const [filters, setFilters] = useState({
    query: '',
    category: '',
    status: writer ? '' : 'published',
  });
  const [page, setPage] = useState<Page>({ items: [], next_cursor: '' });
  const [cursor, setCursor] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [article, setArticle] = useState<Article | null>(null);
  const [editing, setEditing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [editCategory, setEditCategory] = useState('general');
  const [editStatus, setEditStatus] = useState('draft');
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);

  function failure(error: unknown) {
    if (error instanceof APIError && error.status === 401) {
      onExpired();
      return;
    }
    setError(error instanceof Error ? error.message : 'İşlem tamamlanamadı.');
  }
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    const params = new URLSearchParams({
      limit: '20',
      q: filters.query,
      category: filters.category,
      status: filters.status,
    });
    if (cursor) params.set('before', cursor);
    Promise.all([
      request<Page>(`/knowledge?${params}`),
      request<{ items: Category[] }>('/ticket-categories'),
    ])
      .then(([result, cats]) => {
        if (active) {
          setPage(result);
          setCategories(cats.items);
        }
      })
      .catch((error) => {
        if (active) failure(error);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [filters, cursor, attempt, onExpired]);

  function populate(a: Article | null) {
    setTitle(a?.title ?? '');
    setBody(a?.body ?? '');
    setEditCategory(a?.category ?? 'general');
    setEditStatus(a?.status ?? 'draft');
  }
  async function open(id: number) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await request<{ article: Article }>(`/knowledge/${id}`);
      setArticle(result.article);
      setEditing(false);
      setCreating(false);
    } catch (error) {
      failure(error);
    } finally {
      setBusy(false);
    }
  }
  function back() {
    setArticle(null);
    setCreating(false);
    setEditing(false);
    setError('');
    setNotice('');
    setAttempt((v) => v + 1);
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    const input = {
      title: title.trim(),
      body: body.trim(),
      category: editCategory,
      status: editStatus,
      version: article?.version ?? 0,
    };
    const fingerprint = JSON.stringify(input);
    if (pending.current?.fingerprint !== fingerprint)
      pending.current = { fingerprint, key: crypto.randomUUID() };
    try {
      const result = await request<{ article: Article }>(
        article ? `/knowledge/${article.id}` : '/knowledge',
        {
          method: article ? 'PUT' : 'POST',
          body: fingerprint,
          headers: article ? undefined : { 'Idempotency-Key': pending.current.key },
        },
      );
      setArticle(result.article);
      setCreating(false);
      setEditing(false);
      pending.current = null;
      setNotice('Makale kaydedildi.');
      setAttempt((v) => v + 1);
    } catch (error) {
      failure(error);
    } finally {
      setBusy(false);
    }
  }
  const canEdit =
    article &&
    writer &&
    (reviewer || (article.author_id === user.id && article.status === 'draft'));
  const contentLocked = article !== null && article.status !== 'draft';
  return (
    <section className="status-card" aria-labelledby="knowledge-title">
      <div className="section-heading">
        <h2 id="knowledge-title">Bilgi bankası</h2>
        {!article && !creating && writer && (
          <button
            disabled={busy}
            onClick={() => {
              populate(null);
              setCreating(true);
              setError('');
              setNotice('');
            }}
          >
            + Yeni makale
          </button>
        )}
      </div>
      <p className="muted">Sık karşılaşılan sorunlar için çözüm adımlarını bulun.</p>
      {error && (
        <div role="alert">
          <p className="failure">{error}</p>
          <button
            className="secondary"
            disabled={busy}
            onClick={() => (article ? open(article.id) : setAttempt((v) => v + 1))}
          >
            Yeniden yükle
          </button>
        </div>
      )}
      {notice && (
        <p role="status" className="success">
          {notice}
        </p>
      )}
      {busy && <p role="status">İşlem yapılıyor…</p>}
      {article || creating ? (
        <>
          <button className="secondary" disabled={busy} onClick={back}>
            ← Makalelere dön
          </button>
          {creating || editing ? (
            <form onSubmit={save}>
              {contentLocked && (
                <p className="muted">
                  İçeriği değiştirmek için önce durumunu “Taslak” yapıp kaydedin. Düzenlenen taslak
                  yeniden yayımlanmalıdır.
                </p>
              )}
              <label htmlFor="article-title">Makale başlığı</label>
              <input
                id="article-title"
                required
                maxLength={200}
                disabled={busy || contentLocked}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
              />
              <label htmlFor="article-body">Çözüm adımları</label>
              <textarea
                id="article-body"
                required
                rows={10}
                maxLength={10000}
                disabled={busy || contentLocked}
                value={body}
                onChange={(e) => setBody(e.target.value)}
              />
              <label htmlFor="article-category">Makale kategorisi</label>
              <select
                id="article-category"
                disabled={busy || contentLocked}
                value={editCategory}
                onChange={(e) => setEditCategory(e.target.value)}
              >
                {categories.map((c) => (
                  <option key={c.code} value={c.code}>
                    {c.label}
                  </option>
                ))}
              </select>
              {article && reviewer && (
                <>
                  <label htmlFor="article-status">Yayın durumu</label>
                  <select
                    id="article-status"
                    value={editStatus}
                    disabled={busy}
                    onChange={(e) => setEditStatus(e.target.value)}
                  >
                    {Object.entries(statuses)
                      .filter(([code]) => !(article.status === 'archived' && code === 'published'))
                      .map(([code, label]) => (
                        <option key={code} value={code}>
                          {label}
                        </option>
                      ))}
                  </select>
                </>
              )}
              <div className="action-row">
                <button
                  type="submit"
                  disabled={busy || !title.trim() || !body.trim() || !categories.length}
                >
                  {creating ? 'Taslağı oluştur' : 'Makaleyi kaydet'}
                </button>
                <button
                  type="button"
                  className="secondary"
                  disabled={busy}
                  onClick={() => (creating ? back() : setEditing(false))}
                >
                  Vazgeç
                </button>
              </div>
            </form>
          ) : (
            article && (
              <>
                <h3 className="detail-title">{article.title}</h3>
                <div className="ticket-tags">
                  <span className="badge">{statuses[article.status]}</span>
                  <span>
                    {categories.find((c) => c.code === article.category)?.label ?? article.category}
                  </span>
                </div>
                <p className="muted">
                  {article.author_name} · {formatDate(article.updated_at)}
                </p>
                <p className="ticket-description">{article.body}</p>
                <div className="action-row">
                  <button className="secondary" disabled={busy} onClick={() => open(article.id)}>
                    Makaleyi yenile
                  </button>
                  {canEdit && (
                    <button
                      disabled={busy}
                      onClick={() => {
                        populate(article);
                        setEditing(true);
                        setNotice('');
                      }}
                    >
                      Makaleyi düzenle
                    </button>
                  )}
                </div>
              </>
            )
          )}
        </>
      ) : (
        <>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setCursor('');
              setFilters({ query, category, status });
            }}
          >
            <label htmlFor="knowledge-query">Makale ara</label>
            <input
              id="knowledge-query"
              maxLength={200}
              placeholder="Örn. VPN veya yazıcı"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <div className="form-columns">
              <div>
                <label htmlFor="knowledge-category">Kategori filtresi</label>
                <select
                  id="knowledge-category"
                  value={category}
                  onChange={(e) => setCategory(e.target.value)}
                >
                  <option value="">Tüm kategoriler</option>
                  {categories.map((c) => (
                    <option key={c.code} value={c.code}>
                      {c.label}
                    </option>
                  ))}
                </select>
              </div>
              {writer && (
                <div>
                  <label htmlFor="knowledge-status">Durum filtresi</label>
                  <select
                    id="knowledge-status"
                    value={status}
                    onChange={(e) => setStatus(e.target.value)}
                  >
                    <option value="">Tüm durumlar</option>
                    {Object.entries(statuses).map(([code, label]) => (
                      <option key={code} value={code}>
                        {label}
                      </option>
                    ))}
                  </select>
                </div>
              )}
            </div>
            <button type="submit" disabled={loading || busy}>
              Ara
            </button>
          </form>
          {loading ? (
            <p role="status">Makaleler yükleniyor…</p>
          ) : (
            !error && (
              <>
                {page.items.length === 0 && <p>Aramanıza uygun makale bulunamadı.</p>}
                <div className="ticket-list">
                  {page.items.map((a) => (
                    <button
                      key={a.id}
                      className="ticket-row"
                      disabled={busy}
                      onClick={() => open(a.id)}
                    >
                      <strong>{a.title}</strong>
                      <span className="badge">{statuses[a.status]}</span>
                      <span>
                        {categories.find((c) => c.code === a.category)?.label ?? a.category}
                      </span>
                    </button>
                  ))}
                </div>
                <div className="action-row">
                  {cursor && (
                    <button className="secondary" onClick={() => setCursor('')}>
                      İlk sayfa
                    </button>
                  )}
                  {page.next_cursor && (
                    <button className="secondary" onClick={() => setCursor(page.next_cursor)}>
                      Sonraki sayfa
                    </button>
                  )}
                </div>
              </>
            )
          )}
        </>
      )}
    </section>
  );
}

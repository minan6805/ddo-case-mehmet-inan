import { priorityLabels } from './labels';
import { formatDate } from './format';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { APIError, errorMessage, request, type User } from './api';
import { TicketSLA, SLASettings, SLAAlerts } from './SLA';
import { ExpertBoard, AssignmentHistory } from './Assignment';
import { TicketComments } from './Comments';
import { EmailSimulator } from './Email';
import { ThreadTools } from './Threads';

type Ticket = {
  id: number;
  requester_id: number;
  requester_name: string;
  assignee_id: number | null;
  assignee_name: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  category: string;
  created_at: string;
  updated_at: string;
  merged_into_ticket_id: number | null;
};
type Category = { code: string; label: string };
type Page = { items: Ticket[]; next_cursor: string };
const statusLabels: Record<string, string> = {
  open: 'Açık',
  in_progress: 'İşlemde',
  waiting_customer: 'Yanıt bekleniyor',
  resolved: 'Çözüldü',
  closed: 'Kapalı',
};
const transitions: Record<string, string[]> = {
  open: ['in_progress'],
  in_progress: ['waiting_customer', 'resolved'],
  waiting_customer: ['in_progress'],
  resolved: ['in_progress', 'closed'],
  closed: [],
};

function CreateTicket({
  categories,
  onCreated,
  onCancel,
  onExpired,
}: {
  categories: Category[];
  onCreated: (ticket: Ticket) => void;
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [priority, setPriority] = useState('normal');
  const [category, setCategory] = useState(
    categories.find((item) => item.code === 'general')?.code ??
      categories[0]?.code ??
      '',
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const body = {
      title: title.trim(),
      description: description.trim(),
      priority,
      category,
    };
    if (!body.title || !body.description) {
      setError('Başlık ve açıklama boş olamaz.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const fingerprint = JSON.stringify(body);
      if (pending.current?.fingerprint !== fingerprint)
        pending.current = { fingerprint, key: crypto.randomUUID() };
      const result = await request<{ ticket: Ticket }>('/tickets', {
        method: 'POST',
        headers: { 'Idempotency-Key': pending.current.key },
        body: fingerprint,
      });
      onCreated(result.ticket);
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        onExpired();
        return;
      }
      setError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="status-card" aria-labelledby="new-ticket-title">
      <div className="section-heading">
        <h2 id="new-ticket-title">Yeni destek talebi</h2>
        <button
          type="button"
          className="secondary"
          disabled={busy}
          onClick={onCancel}
        >
          Vazgeç
        </button>
      </div>
      <form onSubmit={submit}>
        <label htmlFor="ticket-title">Konu</label>
        <input
          id="ticket-title"
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          maxLength={200}
          required
          disabled={busy}
          placeholder="Örn. Kurum ağına bağlanamıyorum"
        />
        <label htmlFor="ticket-description">Sorununuzu açıklayın</label>
        <textarea
          id="ticket-description"
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          maxLength={10000}
          required
          disabled={busy}
          rows={6}
          placeholder="Ne oldu, ne zamandan beri oluyor ve hangi adımları denediniz?"
        />
        <div className="form-columns">
          <div>
            <label htmlFor="ticket-category">Kategori</label>
            <select
              id="ticket-category"
              value={category}
              onChange={(event) => setCategory(event.target.value)}
              disabled={busy}
              required
            >
              {categories.map((item) => (
                <option key={item.code} value={item.code}>
                  {item.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="ticket-priority">Öncelik</label>
            <select
              id="ticket-priority"
              value={priority}
              onChange={(event) => setPriority(event.target.value)}
              disabled={busy}
            >
              {Object.entries(priorityLabels).map(([code, label]) => (
                <option key={code} value={code}>
                  {label}
                </option>
              ))}
            </select>
          </div>
        </div>
        {error && (
          <p role="alert" className="failure">
            {error}
          </p>
        )}
        <button disabled={busy || !category} type="submit">
          {busy ? 'Kaydediliyor…' : 'Talebi oluştur'}
        </button>
      </form>
    </section>
  );
}

function TicketDetail({
  id,
  staff,
  manager,
  categories,
  onBack,
  onChanged,
  onExpired,
  onOpen,
}: {
  id: number;
  staff: boolean;
  manager: boolean;
  categories: Category[];
  onBack: () => void;
  onChanged: () => void;
  onExpired: () => void;
  onOpen: (id: number) => void;
}) {
  const [ticket, setTicket] = useState<Ticket | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [commentRevision, setCommentRevision] = useState(0);
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    setTicket(null);
    request<{ ticket: Ticket }>(`/tickets/${id}`)
      .then((result) => {
        if (active) setTicket(result.ticket);
      })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) {
          onExpired();
          return;
        }
        setError(errorMessage(error));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [id, attempt, onExpired]);
  async function retryAssignment() {
    setBusy(true);
    setError('');
    try {
      await request('/tickets/' + id + '/assignment', { method: 'PATCH' });
      const result = await request<{ ticket: Ticket }>(`/tickets/${id}`);
      setTicket(result.ticket);
      onChanged();
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        onExpired();
        return;
      }
      setError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  async function updateStatus(status: string) {
    setBusy(true);
    setError('');
    try {
      const result = await request<{ ticket: Ticket }>(
        `/tickets/${id}/status`,
        { method: 'PATCH', body: JSON.stringify({ status }) },
      );
      setTicket(result.ticket);
      onChanged();
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        onExpired();
        return;
      }
      setError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="status-card" aria-labelledby="ticket-detail-title">
      <div className="section-heading">
        <button className="secondary" disabled={busy} onClick={onBack}>
          ← Taleplere dön
        </button>
        <button
          className="secondary"
          disabled={busy || loading}
          onClick={() => setAttempt((value) => value + 1)}
        >
          Talebi yenile
        </button>
      </div>
      {loading && <p role="status">Talep yükleniyor…</p>}
      {error && (
        <div role="alert">
          <p className="failure">{error}</p>
          <button
            disabled={busy}
            onClick={() => setAttempt((value) => value + 1)}
          >
            Talebi yeniden yükle
          </button>
        </div>
      )}
      {ticket && (
        <>
          <p className="eyebrow detail-number">TALEP #{ticket.id}</p>
          <h2 id="ticket-detail-title" className="detail-title">
            {ticket.title}
          </h2>
          <div className="ticket-tags">
            <span className={`badge status-${ticket.status}`}>
              {statusLabels[ticket.status]}
            </span>
            <span className={`priority priority-${ticket.priority}`}>
              {priorityLabels[ticket.priority]}
            </span>
          </div>
          <dl className="ticket-meta">
            <div>
              <dt>Atanan uzman</dt>
              <dd>{ticket.assignee_name || 'Uygun uzman bekleniyor'}</dd>
            </div>
            <div>
              <dt>Talep eden</dt>
              <dd>{ticket.requester_name}</dd>
            </div>
            <div>
              <dt>Kategori</dt>
              <dd>
                {categories.find((item) => item.code === ticket.category)
                  ?.label ?? ticket.category}
              </dd>
            </div>
            <div>
              <dt>Oluşturulma</dt>
              <dd>{formatDate(ticket.created_at)}</dd>
            </div>
            <div>
              <dt>Son güncelleme</dt>
              <dd>{formatDate(ticket.updated_at)}</dd>
            </div>
          </dl>
          {staff &&
            ticket.assignee_id === null &&
            !['resolved', 'closed'].includes(ticket.status) && (
              <button
                className="secondary"
                disabled={busy}
                onClick={retryAssignment}
              >
                Atamayı yeniden dene
              </button>
            )}
          {ticket.merged_into_ticket_id !== null && (
            <p className="success">
              Bu talep #{ticket.merged_into_ticket_id} ile birleştirildi.{' '}
              <button
                className="secondary"
                onClick={() => onOpen(ticket.merged_into_ticket_id!)}
              >
                Birleştirilen talebi aç
              </button>
            </p>
          )}
          <TicketSLA
            id={id}
            revision={ticket.updated_at}
            onExpired={onExpired}
          />
          <h3 className="description-heading">Açıklama</h3>
          <p className="ticket-description">{ticket.description}</p>
          {staff && (transitions[ticket.status] ?? []).length > 0 && (
            <div className="workflow">
              <h3>Talep durumunu değiştir</h3>
              <div className="action-row">
                {transitions[ticket.status].map((next) => (
                  <button
                    key={next}
                    disabled={busy}
                    onClick={() => updateStatus(next)}
                  >
                    {statusLabels[next]}
                  </button>
                ))}
              </div>
            </div>
          )}
          <TicketComments
            onChanged={() => setCommentRevision((v) => v + 1)}
            id={id}
            staff={staff}
            closed={ticket.status === 'closed'}
            revision={ticket.updated_at}
            onExpired={onExpired}
          />
          {staff && (
            <AssignmentHistory
              id={id}
              revision={ticket.updated_at}
              onExpired={onExpired}
            />
          )}
          <ThreadTools
            id={id}
            manager={manager}
            closed={ticket.status === 'closed'}
            revision={`${ticket.updated_at}:${commentRevision}`}
            onExpired={onExpired}
            onOpen={onOpen}
          />
          {ticket.status === 'closed' && (
            <p className="muted">Bu talep kapatıldı.</p>
          )}
        </>
      )}
    </section>
  );
}

export function Tickets({
  user,
  onExpired,
}: {
  user: User;
  onExpired: () => void;
}) {
  const staff = ['specialist', 'team_lead', 'admin'].includes(user.role);
  const [categories, setCategories] = useState<Category[]>([]);
  const [categoriesError, setCategoriesError] = useState('');
  const [categoryAttempt, setCategoryAttempt] = useState(0);
  const [items, setItems] = useState<Ticket[]>([]);
  const [cursor, setCursor] = useState('');
  const [filter, setFilter] = useState('');
  const [queue, setQueue] = useState('all');
  const [detailVersion, setDetailVersion] = useState(0);
  const [loading, setLoading] = useState(true);
  const [moreBusy, setMoreBusy] = useState(false);
  const [error, setError] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [notice, setNotice] = useState('');
  const generation = useRef(0);
  useEffect(() => {
    let active = true;
    setCategoriesError('');
    request<{ items: Category[] }>('/ticket-categories')
      .then((result) => {
        if (active) setCategories(result.items);
      })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) {
          onExpired();
          return;
        }
        setCategoriesError(errorMessage(error));
      });
    return () => {
      active = false;
    };
  }, [categoryAttempt, onExpired]);
  useEffect(() => {
    let active = true;
    generation.current += 1;
    setLoading(true);
    setError('');
    setItems([]);
    setCursor('');
    request<Page>(
      `/tickets?limit=20&status=${encodeURIComponent(filter)}&queue=${encodeURIComponent(queue)}`,
    )
      .then((result) => {
        if (active) {
          setItems(result.items);
          setCursor(result.next_cursor);
        }
      })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) {
          onExpired();
          return;
        }
        setError(errorMessage(error));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [filter, queue, refresh, onExpired]);
  async function loadMore() {
    const currentGeneration = generation.current;
    setMoreBusy(true);
    setError('');
    try {
      const result = await request<Page>(
        `/tickets?limit=20&status=${encodeURIComponent(filter)}&before=${encodeURIComponent(cursor)}&queue=${encodeURIComponent(queue)}`,
      );
      if (currentGeneration === generation.current) {
        setItems((current) => [...current, ...result.items]);
        setCursor(result.next_cursor);
      }
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      if (error instanceof APIError && error.status === 401) {
        onExpired();
        return;
      }
      setError(errorMessage(error));
    } finally {
      setMoreBusy(false);
    }
  }
  return (
    <section aria-labelledby="tickets-title">
      <div className="section-heading ticket-toolbar">
        <div>
          <p className="eyebrow">{staff ? 'DESTEK KUYRUĞU' : 'TALEPLERİM'}</p>
          <h2 id="tickets-title">
            {staff ? 'Destek talepleri' : 'Destek talepleriniz'}
          </h2>
        </div>
        <button
          disabled={!categories.length || creating}
          onClick={() => {
            setCreating(true);
            setSelected(null);
            setNotice('');
          }}
        >
          + Yeni talep
        </button>
      </div>
      {notice && (
        <p className="success" role="status">
          {notice}
        </p>
      )}
      {categoriesError && (
        <div role="alert">
          <p className="failure">Kategoriler alınamadı: {categoriesError}</p>
          <button onClick={() => setCategoryAttempt((value) => value + 1)}>
            Kategorileri tekrar yükle
          </button>
        </div>
      )}
      {creating ? (
        <CreateTicket
          categories={categories}
          onExpired={onExpired}
          onCancel={() => setCreating(false)}
          onCreated={(ticket) => {
            setCreating(false);
            setSelected(ticket.id);
            setNotice(`Talep #${ticket.id} oluşturuldu.`);
            setRefresh((value) => value + 1);
          }}
        />
      ) : selected !== null ? (
        <TicketDetail
          key={`${selected}:${detailVersion}`}
          id={selected}
          staff={staff}
          manager={['admin', 'team_lead'].includes(user.role)}
          onOpen={(id) => {
            setSelected(id);
            setDetailVersion((v) => v + 1);
            setRefresh((v) => v + 1);
          }}
          categories={categories}
          onExpired={onExpired}
          onBack={() => setSelected(null)}
          onChanged={() => setRefresh((value) => value + 1)}
        />
      ) : (
        <div className="status-card ticket-list">
          <div className="section-heading filters">
            {staff && (
              <div>
                <label htmlFor="queue-filter">Kuyruk</label>
                <select
                  id="queue-filter"
                  value={queue}
                  disabled={moreBusy}
                  onChange={(event) => setQueue(event.target.value)}
                >
                  <option value="all">Tüm destek kuyruğu</option>
                  <option value="mine">Bana atananlar</option>
                  <option value="unassigned">Atama bekleyenler</option>
                </select>
              </div>
            )}
            <div>
              <label htmlFor="status-filter">Durum</label>
              <select
                id="status-filter"
                value={filter}
                disabled={moreBusy}
                onChange={(event) => setFilter(event.target.value)}
              >
                <option value="">Tüm durumlar</option>
                {Object.entries(statusLabels).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </div>
            <button
              className="secondary"
              disabled={loading || moreBusy}
              onClick={() => setRefresh((value) => value + 1)}
            >
              Listeyi yenile
            </button>
          </div>
          {loading && <p role="status">Talepler yükleniyor…</p>}
          {error && (
            <div role="alert">
              <p className="failure">{error}</p>
              <button
                className="secondary"
                onClick={() => setRefresh((value) => value + 1)}
              >
                Tekrar dene
              </button>
            </div>
          )}
          {!loading && !error && items.length === 0 && (
            <div className="empty-state">
              <h3>
                {filter ? 'Bu durumda talep bulunamadı' : 'Henüz talep yok'}
              </h3>
              <p>
                {filter
                  ? 'Başka bir durum seçebilir veya yeni talep açabilirsiniz.'
                  : 'Bir sorun mu yaşıyorsunuz? Yeni talep oluşturarak destek ekibine ulaşın.'}
              </p>
            </div>
          )}
          {items.map((ticket) => (
            <button
              className="ticket-row"
              key={ticket.id}
              onClick={() => {
                setSelected(ticket.id);
                setNotice('');
              }}
            >
              <div className="row-top">
                <span className="ticket-number">#{ticket.id}</span>
                <span className={`badge status-${ticket.status}`}>
                  {statusLabels[ticket.status]}
                </span>
              </div>
              <span className="row-title">{ticket.title}</span>
              <span className="row-bottom">
                <span>
                  {staff
                    ? `${ticket.assignee_name || 'Atama bekliyor'} · `
                    : ''}
                  {formatDate(ticket.created_at)}
                </span>
                <span className={`priority priority-${ticket.priority}`}>
                  {priorityLabels[ticket.priority]}
                </span>
              </span>
            </button>
          ))}
          {cursor && (
            <button
              className="secondary"
              disabled={moreBusy || loading}
              onClick={loadMore}
            >
              {moreBusy ? 'Yükleniyor…' : 'Daha fazla göster'}
            </button>
          )}
        </div>
      )}
      {staff && (
        <>
          <EmailSimulator
            user={user}
            onExpired={onExpired}
            onOpenTicket={(id) => {
              setCreating(false);
              setSelected(id);
              setDetailVersion((v) => v + 1);
              setRefresh((v) => v + 1);
              setNotice('');
            }}
          />
          <ExpertBoard
            user={user}
            onExpired={onExpired}
            onChanged={() => {
              setRefresh((value) => value + 1);
              setDetailVersion((value) => value + 1);
            }}
            onQueue={() => {
              setSelected(null);
              setCreating(false);
              setQueue('unassigned');
              setRefresh((value) => value + 1);
            }}
          />
          <SLAAlerts
            onExpired={onExpired}
            onOpenTicket={(id) => {
              setCreating(false);
              setSelected(id);
              setNotice('');
            }}
          />
          <SLASettings user={user} onExpired={onExpired} />
        </>
      )}
    </section>
  );
}

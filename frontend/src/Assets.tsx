import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { APIError, errorMessage, request, type User } from './api';
export type Asset = {
  id: number;
  code: string;
  name: string;
  kind: string;
  state: string;
  owner_id: number | null;
  owner_name: string;
  version: number;
};
export type Impact = {
  graph_version: number;
  items: (Asset & { distance: number; path: number[] })[];
  edges: { provider_id: number; dependent_id: number }[];
};
const kinds: Record<string, string> = {
  server: 'Sunucu',
  service: 'Servis',
  application: 'Uygulama',
  network: 'Ağ',
  device: 'Cihaz',
};
const states: Record<string, string> = {
  active: 'Aktif',
  maintenance: 'Bakımda',
  retired: 'Kullanımdan kaldırıldı',
};
type Detail = { asset: Asset; providers: Asset[]; dependents: Asset[] };
export function ImpactView({ impact }: { impact: Impact }) {
  const arrowId = `impact-arrow-${useId().replace(/:/g, '')}`;
  const nodes = impact.items.slice(0, 50);
  const positions = new Map<number, { x: number; y: number }>();
  const counts = new Map<number, number>();
  for (const node of nodes) {
    const row = counts.get(node.distance) ?? 0;
    counts.set(node.distance, row + 1);
    positions.set(node.id, { x: node.distance * 230 + 20, y: row * 85 + 20 });
  }
  const width = Math.max(700, ...nodes.map((n) => n.distance * 230 + 235));
  const height = Math.max(140, ...[...counts.values()].map((count) => count * 85 + 30));
  return (
    <div className="impact-result">
      <p>
        <strong>{impact.items.length} varlık etkilenebilir</strong> · kök dahil · graf sürümü{' '}
        {impact.graph_version}
      </p>
      <p className="muted">
        Oklar sağlayıcıdan ona bağımlı varlığa gider. Bu liste olası yayılımı gösterir; kesin
        kesinti tahmini değildir.
      </p>
      <div className="graph-scroll">
        <svg
          viewBox={`0 0 ${width} ${height}`}
          style={{ minWidth: width, height }}
          role="img"
          aria-label="Varlık bağımlılık etki grafı"
        >
          <defs>
            <marker
              id={arrowId}
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="#51788a" />
            </marker>
          </defs>
          {impact.edges.map((e) => {
            const a = positions.get(e.provider_id),
              b = positions.get(e.dependent_id);
            return a && b ? (
              <path
                key={`${e.provider_id}:${e.dependent_id}`}
                d={`M ${a.x + 195} ${a.y + 28} C ${a.x + 220} ${a.y + 28}, ${b.x - 25} ${b.y + 28}, ${b.x} ${b.y + 28}`}
                stroke="#51788a"
                fill="none"
                strokeWidth="2"
                markerEnd={`url(#${arrowId})`}
              />
            ) : null;
          })}
          {nodes.map((node) => {
            const p = positions.get(node.id)!;
            return (
              <g key={node.id}>
                <title>
                  {node.name} · {states[node.state]}
                </title>
                <rect
                  x={p.x}
                  y={p.y}
                  width="195"
                  height="58"
                  rx="8"
                  fill={node.distance === 0 ? '#d8eee3' : '#edf3f8'}
                  stroke="#8bb29c"
                />
                <text x={p.x + 10} y={p.y + 23} fontSize="12" fill="#172b4d">
                  {node.code.slice(0, 24)}
                </text>
                <text x={p.x + 10} y={p.y + 43} fontSize="11" fill="#526175">
                  {node.name.slice(0, 25)}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
      {impact.items.length > 50 && (
        <p className="muted">Graf ilk 50 varlığı gösterir; tam etki listesi aşağıdadır.</p>
      )}
      <ul className="impact-list">
        {impact.items.map((n) => (
          <li key={n.id}>
            <strong>{n.code}</strong> · {n.name} · {kinds[n.kind]} · {states[n.state]}{' '}
            <span className="muted">
              ({n.distance === 0 ? 'Seçilen varlık' : `${n.distance} bağlantı uzaklıkta`})
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
export function Assets({ user, onExpired }: { user: User; onExpired: () => void }) {
  const writable = ['asset_manager', 'admin'].includes(user.role);
  const [items, setItems] = useState<Asset[]>([]),
    [cursor, setCursor] = useState('');
  const [filter, setFilter] = useState(''),
    [selected, setSelected] = useState<number | null>(null);
  const [detail, setDetail] = useState<Detail | null>(null),
    [impact, setImpact] = useState<Impact | null>(null);
  const [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0),
    [creating, setCreating] = useState(false);
  const [owners, setOwners] = useState<User[]>([]),
    [ownersCursor, setOwnersCursor] = useState('');
  const [form, setForm] = useState({
    code: '',
    name: '',
    kind: 'server',
    state: 'active',
    owner_id: '',
  });
  const [dependent, setDependent] = useState('');
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);
  function fail(error: unknown) {
    if (error instanceof APIError && error.status === 401) {
      onExpired();
      return;
    }
    setError(errorMessage(error, 'Varlık işlemi tamamlanamadı.'));
  }
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    setItems([]);
    setCursor('');
    request<{ items: Asset[]; next_cursor: string }>(`/assets?limit=20&kind=${filter}`)
      .then((p) => {
        if (active) {
          setItems(p.items);
          setCursor(p.next_cursor);
        }
      })
      .catch((e) => {
        if (active) fail(e);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [filter, revision, onExpired]);
  useEffect(() => {
    if (!writable) return;
    let active = true;
    request<{ items: User[]; next_cursor: string }>('/assets/owners?limit=100')
      .then((p) => {
        if (active) {
          setOwners(p.items);
          setOwnersCursor(p.next_cursor);
        }
      })
      .catch((e) => {
        if (active) fail(e);
      });
    return () => {
      active = false;
    };
  }, [writable, onExpired]);
  useEffect(() => {
    if (selected === null) {
      setDetail(null);
      setImpact(null);
      return;
    }
    let active = true;
    setDetail(null);
    setImpact(null);
    setError('');
    request<Detail>(`/assets/${selected}`)
      .then((d) => {
        if (active) {
          setDetail(d);
          const a = d.asset;
          setForm({
            code: a.code,
            name: a.name,
            kind: a.kind,
            state: a.state,
            owner_id: a.owner_id?.toString() ?? '',
          });
        }
      })
      .catch((e) => {
        if (active) fail(e);
      });
    return () => {
      active = false;
    };
  }, [selected, revision, onExpired]);
  async function more() {
    setBusy(true);
    try {
      const p = await request<{ items: Asset[]; next_cursor: string }>(
        `/assets?limit=20&kind=${filter}&before=${cursor}`,
      );
      setItems((items) => [...items, ...p.items]);
      setCursor(p.next_cursor);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  async function moreOwners() {
    setBusy(true);
    try {
      const p = await request<{ items: User[]; next_cursor: string }>(
        `/assets/owners?limit=100&before=${ownersCursor}`,
      );
      setOwners((items) => [...items, ...p.items]);
      setOwnersCursor(p.next_cursor);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    const body = {
      ...form,
      owner_id: form.owner_id ? Number(form.owner_id) : null,
      version: creating ? 0 : detail?.asset.version,
    };
    try {
      const fingerprint = JSON.stringify(body);
      if (pending.current?.fingerprint !== fingerprint)
        pending.current = { fingerprint, key: crypto.randomUUID() };
      const p = await request<{ asset: Asset }>(creating ? '/assets' : `/assets/${selected}`, {
        method: creating ? 'POST' : 'PUT',
        body: fingerprint,
        headers: creating ? { 'Idempotency-Key': pending.current.key } : undefined,
      });
      pending.current = null;
      setCreating(false);
      setSelected(p.asset.id);
      setRevision((v) => v + 1);
      setNotice('Varlık kaydedildi.');
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  async function link(id: number, add: boolean) {
    if (selected === null) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await request(`/assets/${selected}/dependencies/${id}`, { method: add ? 'PUT' : 'DELETE' });
      setDependent('');
      setRevision((v) => v + 1);
      setNotice(add ? 'Bağımlılık eklendi.' : 'Bağımlılık kaldırıldı.');
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  async function analyze() {
    setBusy(true);
    setError('');
    try {
      setImpact(await request<Impact>(`/assets/${selected}/impact`));
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  const fields = (
    <>
      <label htmlFor="asset-code">Varlık kodu</label>
      <input
        id="asset-code"
        maxLength={40}
        value={form.code}
        required
        disabled={busy}
        onChange={(e) => setForm({ ...form, code: e.target.value })}
        placeholder="SRV-001"
      />
      <label htmlFor="asset-name">Varlık adı</label>
      <input
        id="asset-name"
        maxLength={160}
        value={form.name}
        required
        disabled={busy}
        onChange={(e) => setForm({ ...form, name: e.target.value })}
      />
      <div className="form-columns">
        <div>
          <label htmlFor="asset-kind">Varlık türü</label>
          <select
            id="asset-kind"
            value={form.kind}
            disabled={busy}
            onChange={(e) => setForm({ ...form, kind: e.target.value })}
          >
            {Object.entries(kinds).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="asset-state">Varlık durumu</label>
          <select
            id="asset-state"
            value={form.state}
            disabled={busy}
            onChange={(e) => setForm({ ...form, state: e.target.value })}
          >
            {Object.entries(states).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
      </div>
      <label htmlFor="asset-owner">Sorumlu kişi</label>
      <select
        id="asset-owner"
        value={form.owner_id}
        disabled={busy}
        onChange={(e) => setForm({ ...form, owner_id: e.target.value })}
      >
        <option value="">Atanmamış</option>
        {owners.map((u) => (
          <option key={u.id} value={u.id}>
            {u.display_name} · {u.email}
          </option>
        ))}
        {form.owner_id && !owners.some((u) => String(u.id) === form.owner_id) && (
          <option value={form.owner_id}>{detail?.asset.owner_name || 'Mevcut sorumlu'}</option>
        )}
      </select>
      {ownersCursor && (
        <button type="button" className="secondary" disabled={busy} onClick={moreOwners}>
          Daha fazla sorumlu kişi
        </button>
      )}
    </>
  );
  return (
    <section className="asset-module" aria-labelledby="assets-title">
      <div className="section-heading">
        <div>
          <p className="eyebrow">VARLIK YÖNETİMİ</p>
          <h2 id="assets-title">Varlıklar ve bağımlılıklar</h2>
        </div>
        {writable && (
          <button
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCreating(true);
              setError('');
              setNotice('');
              setForm({
                code: '',
                name: '',
                kind: 'server',
                state: 'active',
                owner_id: String(user.id),
              });
            }}
          >
            + Yeni varlık
          </button>
        )}
      </div>
      {error && (
        <p className="failure" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="success" role="status">
          {notice}
        </p>
      )}
      {creating || selected !== null ? (
        <div className="status-card">
          <button
            className="secondary"
            disabled={busy}
            onClick={() => {
              setCreating(false);
              setSelected(null);
              setError('');
            }}
          >
            ← Varlıklara dön
          </button>
          {creating ? (
            <>
              <h3>Yeni varlık</h3>
              <form onSubmit={save}>
                {fields}
                <button disabled={busy} type="submit">
                  Varlığı oluştur
                </button>
              </form>
            </>
          ) : !detail ? (
            <p role="status">Varlık yükleniyor…</p>
          ) : (
            <>
              <h3>
                {detail.asset.code} · {detail.asset.name}
              </h3>
              <p>
                {kinds[detail.asset.kind]} · {states[detail.asset.state]} ·{' '}
                {detail.asset.owner_name || 'Sorumlu atanmamış'}
              </p>
              {writable && (
                <details>
                  <summary>Varlığı düzenle</summary>
                  <form onSubmit={save}>
                    {fields}
                    <button disabled={busy} type="submit">
                      Varlık bilgilerini kaydet
                    </button>
                  </form>
                </details>
              )}
              <h4>Bu varlığın bağlı olduğu sağlayıcılar</h4>
              {detail.providers.length === 0 && <p className="muted">Sağlayıcı bağlantısı yok.</p>}
              {detail.providers.map((a) => (
                <p key={a.id}>
                  <button className="secondary" disabled={busy} onClick={() => setSelected(a.id)}>
                    {a.code} · {a.name}
                  </button>
                </p>
              ))}
              <h4>Bu varlığa bağımlı olanlar</h4>
              {detail.dependents.length === 0 && <p className="muted">Bağımlı varlık yok.</p>}
              {detail.dependents.map((a) => (
                <div className="section-heading" key={a.id}>
                  <button className="secondary" disabled={busy} onClick={() => setSelected(a.id)}>
                    {a.code} · {a.name}
                  </button>
                  {writable && (
                    <button className="secondary" disabled={busy} onClick={() => link(a.id, false)}>
                      Bağlantıyı kaldır · {a.code}
                    </button>
                  )}
                </div>
              ))}
              {writable && (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void link(Number(dependent), true);
                  }}
                >
                  <label htmlFor="asset-dependent">Yeni bağımlı varlık numarası</label>
                  <input
                    id="asset-dependent"
                    type="number"
                    min={1}
                    value={dependent}
                    disabled={busy}
                    required
                    onChange={(e) => setDependent(e.target.value)}
                  />
                  <p className="muted">
                    Bu varlık sağlayıcı olacak. Eklediğin varlık, bu varlık arızalandığında
                    etkilenebilir. Varlık numaraları listede görünür.
                  </p>
                  <button disabled={busy || !dependent || Number(dependent) === selected}>
                    Bağımlılık ekle
                  </button>
                </form>
              )}
              <div className="workflow">
                <button disabled={busy} onClick={analyze}>
                  Arıza etkisini hesapla
                </button>
                {impact && <ImpactView impact={impact} />}
              </div>
            </>
          )}
        </div>
      ) : (
        <div className="status-card">
          <div className="section-heading filters">
            <div>
              <label htmlFor="asset-filter">Tür filtresi</label>
              <select
                id="asset-filter"
                value={filter}
                disabled={busy}
                onChange={(e) => setFilter(e.target.value)}
              >
                <option value="">Tüm türler</option>
                {Object.entries(kinds).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>
            <button
              className="secondary"
              disabled={busy || loading}
              onClick={() => setRevision((v) => v + 1)}
            >
              Varlıkları yenile
            </button>
          </div>
          {loading && <p role="status">Varlıklar yükleniyor…</p>}
          {!loading && items.length === 0 && <p className="muted">Bu filtrede varlık yok.</p>}
          {items.map((a) => (
            <button
              className="ticket-row"
              key={a.id}
              onClick={() => {
                setSelected(a.id);
                setCreating(false);
                setNotice('');
              }}
            >
              <span className="row-top">
                #{a.id} · {a.code}
                <span className="badge">{states[a.state]}</span>
              </span>
              <span className="row-title">{a.name}</span>
              <span className="row-bottom">
                {kinds[a.kind]} · {a.owner_name || 'Sorumlu atanmamış'}
              </span>
            </button>
          ))}
          {cursor && (
            <button className="secondary" disabled={busy || loading} onClick={more}>
              Daha fazla varlık
            </button>
          )}
        </div>
      )}
    </section>
  );
}

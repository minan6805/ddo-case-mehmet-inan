import { useEffect, useState, type FormEvent } from 'react';
import { APIError, request } from './api';
import { formatDate } from './format';

type Bucket = { code: string; label: string; count: number };
type Expert = {
  id: number;
  name: string;
  capacity: number;
  active: number;
  enabled: boolean;
  on_leave: boolean;
};
type Report = {
  range: { from: string; to: string; category: string; timezone: string };
  snapshot_at: string;
  summary: {
    total: number;
    active: number;
    unassigned_active: number;
    resolved: number;
    closed: number;
  };
  sla: {
    tracked: number;
    missing: number;
    responded: number;
    responded_within_budget: number;
    completed: number;
    completed_within_budget: number;
    average_response_minutes: number | null;
    average_resolution_minutes: number | null;
    warning_tickets: number;
    response_breach_tickets: number;
    resolution_breach_tickets: number;
    escalated_tickets: number;
    oldest_checked_at: string | null;
    latest_checked_at: string | null;
  };
  statuses: Bucket[];
  priorities: Bucket[];
  categories: Bucket[];
  daily: { date: string; count: number }[];
  workload: { total_active: number; unassigned_active: number; experts: Expert[] };
};
type Filters = { from: string; to: string; category: string };
type Category = { code: string; label: string };
function initialFilters(): Filters {
  const to = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Europe/Istanbul' }).format(new Date());
  const start = new Date(`${to}T00:00:00Z`);
  start.setUTCDate(start.getUTCDate() - 29);
  return { from: start.toISOString().slice(0, 10), to, category: '' };
}
function number(value: number | null) {
  return value === null ? '—' : value.toLocaleString('tr-TR', { maximumFractionDigits: 2 });
}
function rate(part: number, total: number) {
  return total === 0 ? '—' : `%${number((part * 100) / total)} (${part}/${total})`;
}
function Breakdown({ title, items, total }: { title: string; items: Bucket[]; total: number }) {
  return (
    <section className="report-breakdown">
      <h3>{title}</h3>
      <ul>
        {items.map((b) => (
          <li key={b.code}>
            <div>
              <span>{b.label}</span>
              <strong>{b.count}</strong>
            </div>
            <meter min={0} max={Math.max(total, 1)} value={b.count} aria-label={b.label}>
              {b.count}
            </meter>
          </li>
        ))}
      </ul>
    </section>
  );
}
export function Reports({ onExpired }: { onExpired: () => void }) {
  const [filters, setFilters] = useState(initialFilters);
  const [applied, setApplied] = useState(filters);
  const [categories, setCategories] = useState<Category[]>([]);
  const [report, setReport] = useState<Report | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    setReport(null);
    Promise.all([
      request<Report>(`/reports?${new URLSearchParams(applied)}`),
      request<{ items: Category[] }>('/ticket-categories'),
    ])
      .then(([result, cats]) => {
        if (active) {
          setReport(result);
          setCategories(cats.items);
        }
      })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) {
          onExpired();
          return;
        }
        setError(error instanceof Error ? error.message : 'Rapor hazırlanamadı.');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [applied, attempt, onExpired]);
  function apply(event: FormEvent) {
    event.preventDefault();
    const days = (Date.parse(filters.to) - Date.parse(filters.from)) / 86400000;
    if (!Number.isFinite(days) || days < 0 || days >= 366) {
      setError('Başlangıç ve bitiş arasında en fazla 366 gün seçin.');
      return;
    }
    setApplied({ ...filters });
  }
  return (
    <section className="status-card reports-module" aria-labelledby="reports-title">
      <div className="section-heading">
        <h2 id="reports-title">Raporlar</h2>
        <button className="secondary" disabled={loading} onClick={() => setAttempt((v) => v + 1)}>
          Raporu yenile
        </button>
      </div>
      <p>Seçilen tarihlerde oluşturulan taleplerin güncel durumunu inceleyin.</p>
      <form onSubmit={apply}>
        <div className="form-columns">
          <div>
            <label htmlFor="report-from">Başlangıç tarihi</label>
            <input
              id="report-from"
              type="date"
              required
              value={filters.from}
              onChange={(e) => setFilters({ ...filters, from: e.target.value })}
            />
          </div>
          <div>
            <label htmlFor="report-to">Bitiş tarihi</label>
            <input
              id="report-to"
              type="date"
              required
              value={filters.to}
              onChange={(e) => setFilters({ ...filters, to: e.target.value })}
            />
          </div>
        </div>
        <label htmlFor="report-category">Rapor kategorisi</label>
        <select
          id="report-category"
          value={filters.category}
          onChange={(e) => setFilters({ ...filters, category: e.target.value })}
        >
          <option value="">Tüm kategoriler</option>
          {categories.map((c) => (
            <option key={c.code} value={c.code}>
              {c.label}
            </option>
          ))}
        </select>
        <button type="submit" disabled={loading}>
          Raporu göster
        </button>
      </form>
      {loading && <p role="status">Rapor hazırlanıyor…</p>}
      {error && (
        <p className="failure" role="alert">
          {error}
        </p>
      )}
      {report && (
        <>
          <p className="muted">
            {report.range.from} – {report.range.to} (iki tarih de dahil, İstanbul saati) ·{' '}
            {categories.find((c) => c.code === report.range.category)?.label ?? 'Tüm kategoriler'}
          </p>
          <p className="muted">Rapor zamanı: {formatDate(report.snapshot_at, 'Europe/Istanbul')}</p>
          {report.summary.total === 0 && (
            <p role="status">Seçilen tarih ve kategoride talep bulunamadı.</p>
          )}
          <dl className="report-cards" aria-label="Talep özeti">
            {[
              ['Toplam talep', report.summary.total],
              ['Aktif talep', report.summary.active],
              ['Atanmamış aktif', report.summary.unassigned_active],
              ['Çözüldü', report.summary.resolved],
              ['Kapalı', report.summary.closed],
            ].map(([label, value]) => (
              <div key={label}>
                <dt>{label}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
          <div className="report-breakdowns">
            <Breakdown title="Duruma göre" items={report.statuses} total={report.summary.total} />
            <Breakdown
              title="Önceliğe göre"
              items={report.priorities}
              total={report.summary.total}
            />
            <Breakdown
              title="Kategoriye göre"
              items={report.categories}
              total={report.summary.total}
            />
          </div>
          <h3>SLA sonuçları</h3>
          <p className="muted">
            Kayıtlı iş süresi sayaçları kullanılır. Ortalamalara yalnız ilk yanıtı verilmiş veya
            çözümü tamamlanmış talepler girer. Hedef sürenin tam dolması ihlaldir.
          </p>
          <dl className="report-metrics">
            <div>
              <dt>SLA kaydı bulunan / eksik</dt>
              <dd>
                {report.sla.tracked} / {report.sla.missing}
              </dd>
            </div>
            <div>
              <dt>Hedef süreden önce ilk yanıt</dt>
              <dd>{rate(report.sla.responded_within_budget, report.sla.responded)}</dd>
            </div>
            <div>
              <dt>Hedef süreden önce çözüm</dt>
              <dd>{rate(report.sla.completed_within_budget, report.sla.completed)}</dd>
            </div>
            <div>
              <dt>Ortalama ilk yanıt (iş dakikası)</dt>
              <dd>
                {number(report.sla.average_response_minutes)} · {report.sla.responded} örnek
              </dd>
            </div>
            <div>
              <dt>Ortalama çözüm (iş dakikası)</dt>
              <dd>
                {number(report.sla.average_resolution_minutes)} · {report.sla.completed} örnek
              </dd>
            </div>
            <div>
              <dt>Uyarı kaydı bulunan talep</dt>
              <dd>{report.sla.warning_tickets}</dd>
            </div>
            <div>
              <dt>İlk yanıt ihlali kaydı bulunan talep</dt>
              <dd>{report.sla.response_breach_tickets}</dd>
            </div>
            <div>
              <dt>Çözüm ihlali kaydı bulunan talep</dt>
              <dd>{report.sla.resolution_breach_tickets}</dd>
            </div>
            <div>
              <dt>Eskalasyon kaydı bulunan talep</dt>
              <dd>{report.sla.escalated_tickets}</dd>
            </div>
          </dl>
          <p className="muted">
            Uyarı ve ihlaller geçmişte kaydedilen olaylardır; güncel sayaçla farklı olabilir. Bir
            talep birden fazla olay üretse de her göstergede bir kez sayılır.
          </p>
          <p className="muted">
            SLA kontrol zamanları:{' '}
            {report.sla.oldest_checked_at
              ? formatDate(report.sla.oldest_checked_at, 'Europe/Istanbul')
              : '—'}{' '}
            –{' '}
            {report.sla.latest_checked_at
              ? formatDate(report.sla.latest_checked_at, 'Europe/Istanbul')
              : '—'}
            . Rapor açmak sayaçları yeniden hesaplamaz.
          </p>
          <h3>Günlük oluşturulan talepler</h3>
          <div className="report-table-wrap report-daily">
            <table>
              <caption>İstanbul tarihine göre talep sayısı</caption>
              <thead>
                <tr>
                  <th scope="col">Tarih</th>
                  <th scope="col">Talep</th>
                </tr>
              </thead>
              <tbody>
                {report.daily.map((d) => (
                  <tr key={d.date}>
                    <th scope="row">{d.date}</th>
                    <td>{d.count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <h3>Güncel uzman iş yükü</h3>
          <p className="muted">
            Bu bölüm tarih ve kategori filtresinden bağımsızdır; sistemdeki tüm aktif talepleri
            gösterir. Aktif durumlar: açık, işlemde ve yanıt bekleniyor.
          </p>
          <p>
            Toplam aktif: <strong>{report.workload.total_active}</strong> · Atanmamış:{' '}
            <strong>{report.workload.unassigned_active}</strong>
          </p>
          {report.workload.experts.length === 0 ? (
            <p>Uzman profili bulunamadı.</p>
          ) : (
            <div className="report-table-wrap">
              <table>
                <caption>Uzmanlara atanmış aktif talepler</caption>
                <thead>
                  <tr>
                    <th scope="col">Uzman</th>
                    <th scope="col">Yük / kapasite</th>
                    <th scope="col">Profil</th>
                  </tr>
                </thead>
                <tbody>
                  {report.workload.experts.map((e) => (
                    <tr key={e.id}>
                      <th scope="row">{e.name}</th>
                      <td>
                        {e.active} / {e.capacity}
                      </td>
                      <td>{!e.enabled ? 'Devre dışı' : e.on_leave ? 'İzinli' : 'Etkin'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </section>
  );
}

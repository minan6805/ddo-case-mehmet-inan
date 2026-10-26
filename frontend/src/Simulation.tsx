import { useEffect, useState } from 'react';
import { APIError, errorMessage, request } from './api';
import { formatDate } from './format';

type Run = {
  id: number;
  config: { name: string; day: string; seed: number; count: number; day_seconds: number };
  status: string;
  total_steps: number;
  completed_steps: number;
  generated_tickets: number;
  email_replays: number;
  reply_deliveries: number;
  shift_steps: number;
  started_at: string;
  updated_at: string;
  completed_at: string | null;
  last_error: string;
};
type Page = { items: Run[]; next_cursor: string };
const statuses: Record<string, string> = {
  running: 'Çalışıyor',
  completed: 'Tamamlandı',
  failed: 'Kesildi',
};
const errors: Record<string, string> = {
  WORKLOAD_FAILED: 'Çalışma tamamlanamadı. Aynı ayarlarla yeniden başlatılarak devam edilebilir.',
  PROFILE_CHANGED:
    'Uzman profili dışarıdan değiştirildi. Bu değişiklik korunarak çalışma durduruldu.',
  RUN_INTERRUPTED: 'Veri üretimi durduruldu. Aynı ayarlarla kaldığı yerden devam edilebilir.',
};
export function Simulation({ onExpired }: { onExpired: () => void }) {
  const [page, setPage] = useState<Page>({ items: [], next_cursor: '' });
  const [cursor, setCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    let pending = false;
    setLoading(true);
    setError('');
    async function refresh() {
      if (pending) return;
      pending = true;
      try {
        const result = await request<Page>(
          `/simulations?limit=10${cursor ? `&before=${cursor}` : ''}`,
        );
        if (active) {
          setPage(result);
          setError('');
        }
      } catch (error) {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) {
          onExpired();
          return;
        }
        setError(errorMessage(error, 'Simülasyon durumu alınamadı.'));
      } finally {
        pending = false;
        if (active) setLoading(false);
      }
    }
    void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [cursor, attempt, onExpired]);
  return (
    <section className="status-card simulation-module" aria-labelledby="simulation-title">
      <div className="section-heading">
        <h2 id="simulation-title">Simülasyon durumu</h2>
        <button className="secondary" disabled={loading} onClick={() => setAttempt((v) => v + 1)}>
          Durumu yenile
        </button>
      </div>
      <p className="muted">
        Durum beş saniyede bir güncellenir. Oluşturulan talepler, mesajlar ve varlıklar ilgili
        ekranlarda görülebilir.
      </p>
      {loading && <p role="status">Simülasyonlar yükleniyor…</p>}
      {error && (
        <p className="failure" role="alert">
          {error}
        </p>
      )}
      {!loading && !error && page.items.length === 0 && <p>Henüz simülasyon çalıştırılmadı.</p>}
      {!loading &&
        page.items.map((run) => {
          const stale = run.status === 'running' && Date.now() - Date.parse(run.updated_at) > 30000;
          return (
            <article key={run.id} className="simulation-run" aria-label={`Çalışma ${run.id}`}>
              <div className="section-heading">
                <h3>{run.config.name}</h3>
                <span className="badge">
                  {stale ? 'Durum güncellenmiyor' : (statuses[run.status] ?? run.status)}
                </span>
              </div>
              <p className="muted">
                Plan günü: {run.config.day} · Tohum: {run.config.seed} · Hedef: {run.config.count}{' '}
                talep
              </p>
              <p className="muted">
                {run.config.day_seconds === 0
                  ? 'Beklemesiz örnek üretimi.'
                  : `Bir günlük talep akışı ${run.config.day_seconds} saniyeye yayılır.`}{' '}
                Kayıtlar gerçek oluşturulma saatini kullanır.
              </p>
              <label htmlFor={`simulation-progress-${run.id}`}>
                İşlem ilerlemesi: {run.completed_steps} / {run.total_steps} (%
                {Math.floor((run.completed_steps * 100) / run.total_steps)})
              </label>
              <progress
                id={`simulation-progress-${run.id}`}
                value={run.completed_steps}
                max={run.total_steps}
              >
                {run.completed_steps} / {run.total_steps}
              </progress>
              <dl className="report-cards simulation-metrics">
                <div>
                  <dt>Üretilen talep</dt>
                  <dd>
                    {run.generated_tickets} / {run.config.count}
                  </dd>
                </div>
                <div>
                  <dt>Tekrarı önlenen e-posta</dt>
                  <dd>{run.email_replays}</dd>
                </div>
                <div>
                  <dt>Yanıt zinciri</dt>
                  <dd>{run.reply_deliveries}</dd>
                </div>
                <div>
                  <dt>Vardiya adımı</dt>
                  <dd>{run.shift_steps}</dd>
                </div>
              </dl>
              {stale && (
                <p role="status">
                  Veri üreticisinden yakın zamanda güncelleme alınmadı. Çalıştırma kesilmiş
                  olabilir.
                </p>
              )}
              {run.last_error && (
                <p className="failure">{errors[run.last_error] ?? 'Çalışma tamamlanamadı.'}</p>
              )}
              <p className="muted">
                Başlangıç: {formatDate(run.started_at, 'Europe/Istanbul')}
                <br />
                Son güncelleme: {formatDate(run.updated_at, 'Europe/Istanbul')}
                {run.completed_at && (
                  <>
                    <br />
                    Bitiş: {formatDate(run.completed_at, 'Europe/Istanbul')}
                  </>
                )}
              </p>
            </article>
          );
        })}
      <div className="action-row">
        {cursor && (
          <button className="secondary" disabled={loading} onClick={() => setCursor('')}>
            İlk sayfa
          </button>
        )}
        {page.next_cursor && (
          <button
            className="secondary"
            disabled={loading}
            onClick={() => setCursor(page.next_cursor)}
          >
            Daha eski çalışmalar
          </button>
        )}
      </div>
    </section>
  );
}

import { useEffect, useState } from 'react';
import { APIError, request, type User } from './api';

type Timer = { budget_seconds: number; elapsed_seconds: number; due_at: string | null; completed_at: string | null; state: string; percent: number };
type Snapshot = { response: Timer; resolution: Timer; paused: boolean; calendar_version: number; checked_at: string; escalation_level: number };
type Holiday = { date: string; closed_from_minute: number; label: string };
type Calendar = { timezone: string; start_minute: number; end_minute: number; version: number; holidays: Holiday[] };
type Policy = { category: string; priority: string; response_minutes: number; resolution_minutes: number; version: number };
type Settings = { calendar: Calendar; policies: Policy[] };
type Alert = { id: number; ticket_id: number; title: string; timer: string; stage: string; recipient_role: string; created_at: string };
const states: Record<string, string> = { running: 'Süre devam ediyor', completed: 'Tamamlandı', paused: 'Duraklatıldı', warning: '%80 uyarısı', breached: 'Süre aşıldı', stopped: 'Durduruldu' };
const priorities: Record<string, string> = { low: 'Düşük', normal: 'Normal', high: 'Yüksek', critical: 'Kritik' };
const categories: Record<string, string> = { general: 'Genel destek', network: 'Ağ ve bağlantı', hardware: 'Donanım', software: 'Yazılım' };
const dateTime = (value: string) => new Intl.DateTimeFormat('tr-TR', { dateStyle: 'short', timeStyle: 'short', timeZone: 'Europe/Istanbul' }).format(new Date(value));
const message = (error: unknown) => error instanceof Error ? error.message : 'İşlem tamamlanamadı.';
const toTime = (minute: number) => `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`;
const toMinute = (value: string) => { const [hour, minute] = value.split(':').map(Number); return hour * 60 + minute; };

function TimerCard({ title, timer }: { title: string; timer: Timer }) {
  return <div className={`sla-timer sla-${timer.state}`}>
    <div className="section-heading"><h4>{title}</h4><span className="sla-state">{states[timer.state] ?? timer.state}</span></div>
    <p className="sla-usage">{Math.floor(timer.elapsed_seconds / 60)} / {timer.budget_seconds / 60} iş dakikası</p>
    <progress value={timer.percent} max={100} aria-label={`${title}: kullanılan süre yüzde ${timer.percent}`} />
    <p className="sla-deadline">{timer.completed_at ? `Tamamlanma: ${dateTime(timer.completed_at)}` : timer.due_at ? `Hedef: ${dateTime(timer.due_at)} (İstanbul)` : 'Devam ettiğinde hedef zaman yeniden hesaplanacak.'}</p>
  </div>;
}

export function TicketSLA({ id, revision, onExpired }: { id: number; revision: string; onExpired: () => void }) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setSnapshot(null); setError('');
    const load = () => request<{ sla: Snapshot }>(`/tickets/${id}/sla`)
      .then((result) => { if (active) { setSnapshot(result.sla); setError(''); } })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) { onExpired(); return; }
        setError(message(error));
      });
    void load();
    const timer = setInterval(() => { void load(); }, 15000);
    return () => { active = false; clearInterval(timer); };
  }, [id, revision, attempt, onExpired]);
  return <section className="sla-section" aria-labelledby="sla-title">
    <h3 id="sla-title">Hizmet süresi (SLA)</h3>
    <p className="muted small-text">Süreler iş takvimine göre sayılır. İlk işleme alma, kabul yanıtı olarak kaydedilir.</p>
    {!snapshot && !error && <p role="status">SLA hesaplanıyor…</p>}
    {error && <div role="alert"><p className="failure">{error}</p><button className="secondary" onClick={() => setAttempt((value) => value + 1)}>Tekrar dene</button></div>}
    {snapshot && <>
      {snapshot.paused && <p className="sla-paused" role="status">Kullanıcı yanıtı bekleniyor. Çözüm süresi duraklatıldı.</p>}
      {snapshot.escalation_level > 0 && <p className="failure" role="status">SLA ihlali takım liderine iletildi.</p>}
      <div className="sla-grid"><TimerCard title="İlk yanıt" timer={snapshot.response} /><TimerCard title="Çözüm" timer={snapshot.resolution} /></div>
      <p className="small-text muted">Son kontrol: {dateTime(snapshot.checked_at)} · Ekran 15 saniyede bir yenilenir.</p>
      {snapshot.response.completed_at && <p className="small-text">Destek ekibi {dateTime(snapshot.response.completed_at)} tarihinde talebinizi işleme aldı.</p>}
    </>}
  </section>;
}

function PolicyRow({ policy, editable, onSaved, onExpired }: { policy: Policy; editable: boolean; onSaved: (policy: Policy) => void; onExpired: () => void }) {
  const [response, setResponse] = useState(policy.response_minutes);
  const [resolution, setResolution] = useState(policy.resolution_minutes);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => { setResponse(policy.response_minutes); setResolution(policy.resolution_minutes); }, [policy]);
  async function save() {
    setBusy(true); setError('');
    try {
      const result = await request<{ policy: Policy }>(`/sla/policies/${policy.category}/${policy.priority}`, { method: 'PUT', body: JSON.stringify({ response_minutes: response, resolution_minutes: resolution, version: policy.version }) });
      onSaved(result.policy);
    } catch (error) {
      if (error instanceof APIError && error.status === 401) { onExpired(); return; }
      setError(message(error));
    } finally { setBusy(false); }
  }
  const label = `${categories[policy.category] ?? policy.category}, ${priorities[policy.priority] ?? policy.priority}`;
  return <div className="sla-policy-row">
    <strong>{label}</strong>
    <div><label>Yanıt (iş dk.){editable ? <input aria-label={`${label} yanıt dakikası`} type="number" min={1} max={43200} value={response} disabled={busy} onChange={(event) => setResponse(Number(event.target.value))} /> : <span>{response}</span>}</label></div>
    <div><label>Çözüm (iş dk.){editable ? <input aria-label={`${label} çözüm dakikası`} type="number" min={1} max={43200} value={resolution} disabled={busy} onChange={(event) => setResolution(Number(event.target.value))} /> : <span>{resolution}</span>}</label></div>
    {editable && <button className="secondary" disabled={busy || !Number.isInteger(response) || !Number.isInteger(resolution) || response < 1 || response > 43200 || resolution < 1 || resolution > 43200} onClick={save}>{busy ? 'Kaydediliyor…' : 'Kaydet'}</button>}
    {error && <p role="alert" className="failure">{error}</p>}
  </div>;
}

export function SLASettings({ user, onExpired }: { user: User; onExpired: () => void }) {
  const [opened, setOpened] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [calendar, setCalendar] = useState<Calendar | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [holidayDate, setHolidayDate] = useState('');
  const [holidayLabel, setHolidayLabel] = useState('');
  const [halfDay, setHalfDay] = useState(false);
  const admin = user.role === 'admin';
  useEffect(() => {
    if (!opened) return;
    let active = true;
    setError('');
    request<Settings>('/sla/settings')
      .then((result) => { if (active) { setSettings(result); setCalendar(result.calendar); } })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) { onExpired(); return; }
        setError(message(error));
      });
    return () => { active = false; };
  }, [opened, attempt, onExpired]);
  async function saveCalendar() {
    if (!calendar) return;
    setBusy(true); setError(''); setNotice('');
    try {
      const result = await request<{ calendar: Calendar }>('/sla/calendar', { method: 'PUT', body: JSON.stringify(calendar) });
      setCalendar(result.calendar);
      setSettings((current) => current ? { ...current, calendar: result.calendar } : current);
      setNotice('Takvim kaydedildi ve talep süreleri yeniden hesaplandı.');
    } catch (error) {
      if (error instanceof APIError && error.status === 401) { onExpired(); return; }
      setError(message(error));
    } finally { setBusy(false); }
  }
  function addHoliday() {
    if (!calendar || !holidayDate || !holidayLabel.trim()) return;
    if (calendar.holidays.some((holiday) => holiday.date === holidayDate)) { setError('Bu tarih zaten takvimde var.'); return; }
    setCalendar({ ...calendar, holidays: [...calendar.holidays, { date: holidayDate, closed_from_minute: halfDay ? 780 : 0, label: holidayLabel.trim() }].sort((a, b) => a.date.localeCompare(b.date)) });
    setHolidayDate(''); setHolidayLabel(''); setError(''); setNotice('Tatil listeye eklendi. Uygulamak için takvimi kaydedin.');
  }
  return <details className="status-card sla-settings" onToggle={(event) => setOpened(event.currentTarget.open)}>
    <summary>SLA takvimi ve hedefleri</summary>
    {opened && <>
      {!settings && !error && <p role="status">Ayarlar yükleniyor…</p>}
      {error && <div role="alert"><p className="failure">{error}</p><button className="secondary" disabled={busy} onClick={() => { setSettings(null); setCalendar(null); setAttempt((value) => value + 1); }}>Ayarları yeniden yükle</button></div>}
      {notice && <p className="success" role="status">{notice}</p>}
      {calendar && settings && <>
        <h3>İş takvimi</h3><p className="small-text muted">Hafta içi · {calendar.timezone}. 2026 tatilleri hazırdır; sonraki yılların tatillerini ayrıca ekleyin.</p>
        <div className="form-columns"><label>Mesai başlangıcı<input type="time" disabled={!admin || busy} value={toTime(calendar.start_minute)} onChange={(event) => setCalendar({ ...calendar, start_minute: toMinute(event.target.value) })} /></label><label>Mesai bitişi<input type="time" disabled={!admin || busy} value={toTime(calendar.end_minute)} onChange={(event) => setCalendar({ ...calendar, end_minute: toMinute(event.target.value) })} /></label></div>
        <ul className="holiday-list">{calendar.holidays.map((holiday) => <li key={holiday.date}><span><strong>{holiday.date}</strong> · {holiday.label}<small>{holiday.closed_from_minute === 0 ? 'Tam gün kapalı' : `${toTime(holiday.closed_from_minute)} sonrası kapalı`}</small></span>{admin && <button className="secondary" aria-label={`${holiday.date} tatilini kaldır`} disabled={busy} onClick={() => setCalendar({ ...calendar, holidays: calendar.holidays.filter((item) => item.date !== holiday.date) })}>Kaldır</button>}</li>)}</ul>
        {admin && <>
          <div className="holiday-add"><label>Tatil tarihi<input type="date" value={holidayDate} disabled={busy} onChange={(event) => setHolidayDate(event.target.value)} /></label><label>Açıklama<input value={holidayLabel} maxLength={120} disabled={busy} onChange={(event) => setHolidayLabel(event.target.value)} /></label><label className="checkbox-label"><input type="checkbox" checked={halfDay} disabled={busy} onChange={(event) => setHalfDay(event.target.checked)} />13.00 sonrası kapalı</label><button className="secondary" disabled={busy || !holidayDate || !holidayLabel.trim()} onClick={addHoliday}>Tatil ekle</button></div>
          <button disabled={busy || !Number.isInteger(calendar.start_minute) || !Number.isInteger(calendar.end_minute) || calendar.start_minute >= calendar.end_minute} onClick={saveCalendar}>{busy ? 'Hesaplanıyor…' : 'Takvimi kaydet ve yeniden hesapla'}</button>
        </>}
        <h3 className="policy-heading">Kategori ve öncelik hedefleri</h3><p className="small-text muted">Hedef değişiklikleri yeni taleplere uygulanır. Mevcut talepler açıldıkları andaki hedefi korur.</p>
        {settings.policies.map((policy) => <PolicyRow key={`${policy.category}:${policy.priority}`} policy={policy} editable={admin} onExpired={onExpired} onSaved={(updated) => { setSettings((current) => current ? { ...current, policies: current.policies.map((item) => item.category === updated.category && item.priority === updated.priority ? updated : item) } : current); setNotice('Hedef yeni talepler için kaydedildi.'); }} />)}
      </>}
    </>}
  </details>;
}

export function SLAAlerts({ onExpired, onOpenTicket }: { onExpired: () => void; onOpenTicket: (id: number) => void }) {
  const [items, setItems] = useState<Alert[]>([]);
  const [cursor, setCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setLoading(true); setError('');
    request<{ items: Alert[]; next_cursor: string }>('/sla/alerts?limit=20')
      .then((result) => { if (active) { setItems(result.items); setCursor(result.next_cursor); } })
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 401) { onExpired(); return; }
        setError(message(error));
      }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [attempt, onExpired]);
  async function loadMore() {
    setLoading(true); setError('');
    try {
      const result = await request<{ items: Alert[]; next_cursor: string }>(`/sla/alerts?limit=20&before=${encodeURIComponent(cursor)}`);
      setItems((current) => [...current, ...result.items]); setCursor(result.next_cursor);
    } catch (error) {
      if (error instanceof APIError && error.status === 401) { onExpired(); return; }
      setError(message(error));
    } finally { setLoading(false); }
  }
  const stageLabels: Record<string, string> = { warning: '%80 uyarısı', breach: 'Süre ihlali', escalation: 'Takım liderine eskalasyon' };
  return <section className="status-card" aria-labelledby="sla-alerts-title">
    <div className="section-heading"><h2 id="sla-alerts-title">SLA uyarıları</h2><button className="secondary" disabled={loading} onClick={() => setAttempt((value) => value + 1)}>Yenile</button></div>
    {loading && <p role="status">Uyarılar yükleniyor…</p>}
    {error && <p role="alert" className="failure">{error}</p>}
    {!loading && !error && items.length === 0 && <p className="muted">Henüz SLA uyarısı yok. Eşik aşıldığında burada görünecek.</p>}
    {items.map((alert) => <button className="ticket-row" key={alert.id} onClick={() => onOpenTicket(alert.ticket_id)}><span className="row-top"><strong>#{alert.ticket_id} · {alert.timer === 'response' ? 'İlk yanıt' : 'Çözüm'}</strong><span className={alert.stage === 'warning' ? 'priority-high' : 'failure'}>{stageLabels[alert.stage]}</span></span><span className="row-title">{alert.title}</span><span className="small-text">{dateTime(alert.created_at)} · Takım lideri</span></button>)}
    {cursor && <button className="secondary" disabled={loading} onClick={loadMore}>Daha fazla uyarı</button>}
  </section>;
}

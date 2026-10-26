import { timeToMinutes } from './format';
import { formatDate as dateTime } from './format';
import { useEffect, useState } from 'react';
import { APIError, errorMessage, request, type User } from './api';

type Skill = { code: string; level: number };
type SkillInfo = { code: string; label: string };
type Expert = {
  id: number;
  display_name: string;
  email: string;
  capacity: number;
  load: number;
  enabled: boolean;
  on_leave: boolean;
  timezone: string;
  shift_start_minute: number;
  shift_end_minute: number;
  weekdays: number[];
  skills: Skill[];
  on_shift: boolean;
  version: number;
};
type ProfileInput = Pick<
  Expert,
  | 'capacity'
  | 'enabled'
  | 'on_leave'
  | 'timezone'
  | 'shift_start_minute'
  | 'shift_end_minute'
  | 'weekdays'
  | 'skills'
  | 'version'
>;
type Board = { experts: Expert[]; skills: SkillInfo[]; queued: number };
type History = {
  id: number;
  from_name: string;
  to_name: string;
  actor_name: string;
  reason: string;
  created_at: string;
  score: {
    total?: number;
    skill_points?: number;
    spare_points?: number;
    rotation_points?: number;
  };
};
const timeValue = (minutes: number) =>
  `${String(Math.floor((minutes % 1440) / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;
function inputOf(expert: Expert): ProfileInput {
  return {
    capacity: expert.capacity,
    enabled: expert.enabled,
    on_leave: expert.on_leave,
    timezone: expert.timezone,
    shift_start_minute: expert.shift_start_minute,
    shift_end_minute: expert.shift_end_minute,
    weekdays: expert.weekdays,
    skills: expert.skills,
    version: expert.version,
  };
}

function ExpertCard({
  expert,
  catalog,
  editable,
  onSaved,
  onExpired,
}: {
  expert: Expert;
  catalog: SkillInfo[];
  editable: boolean;
  onSaved: (board: Board) => void;
  onExpired: () => void;
}) {
  const [input, setInput] = useState<ProfileInput>(inputOf(expert));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // Load/rotation refreshes should not discard unsaved profile edits.
  useEffect(() => {
    setInput(inputOf(expert));
  }, [expert.id, expert.version]);
  async function save() {
    setBusy(true);
    setError('');
    try {
      const result = await request<Board>(`/assignment/experts/${expert.id}`, {
        method: 'PUT',
        body: JSON.stringify(input),
      });
      onSaved(result);
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
  const allDay =
    input.shift_start_minute === 0 && input.shift_end_minute === 1440;
  const availability = !expert.enabled
    ? 'Atama kapalı'
    : expert.on_leave
      ? 'İzinde'
      : !expert.on_shift
        ? 'Vardiya dışında'
        : expert.load >= expert.capacity
          ? 'Kapasite dolu'
          : 'Atamaya uygun';
  return (
    <article
      className="expert-card"
      aria-label={`${expert.display_name} uzman profili`}
    >
      <div className="section-heading">
        <h3>{expert.display_name}</h3>
        <span className="badge">{availability}</span>
      </div>
      <p className="small-text muted">{expert.email}</p>
      <p className="expert-load">
        <strong>
          {expert.load} / {expert.capacity}
        </strong>{' '}
        aktif talep
      </p>
      <progress
        value={expert.load}
        max={expert.capacity}
        aria-label={`${expert.display_name} aktif yükü`}
      />
      {!editable ? (
        <>
          <p className="small-text">
            {expert.shift_start_minute === 0 && expert.shift_end_minute === 1440
              ? 'Tüm gün'
              : `${timeValue(expert.shift_start_minute)}–${timeValue(expert.shift_end_minute)}`}{' '}
            ·{' '}
            {expert.weekdays
              .map(
                (day) =>
                  ['Pzt', 'Sal', 'Çar', 'Per', 'Cum', 'Cmt', 'Paz'][day - 1],
              )
              .join(', ')}
          </p>
          <p className="small-text">
            {expert.skills.length
              ? expert.skills
                  .map(
                    (skill) =>
                      `${catalog.find((item) => item.code === skill.code)?.label ?? skill.code} (${skill.level}/5)`,
                  )
                  .join(' · ')
              : 'Beceri tanımlanmamış'}
          </p>
        </>
      ) : (
        <>
          <div className="form-columns">
            <label>
              Yük limiti
              <input
                type="number"
                min={1}
                max={100}
                value={input.capacity}
                disabled={busy}
                onChange={(event) =>
                  setInput({ ...input, capacity: Number(event.target.value) })
                }
              />
            </label>
            <label>
              Saat dilimi
              <select
                value={input.timezone}
                disabled={busy}
                onChange={(event) =>
                  setInput({ ...input, timezone: event.target.value })
                }
              >
                <option value="Europe/Istanbul">İstanbul (UTC+3)</option>
                <option value="UTC">UTC</option>
                {!['UTC', 'Europe/Istanbul'].includes(input.timezone) && (
                  <option value={input.timezone}>{input.timezone}</option>
                )}
              </select>
            </label>
          </div>
          <div className="expert-flags">
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={input.enabled}
                disabled={busy}
                onChange={(event) =>
                  setInput({ ...input, enabled: event.target.checked })
                }
              />
              Atamaya açık
            </label>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={input.on_leave}
                disabled={busy}
                onChange={(event) =>
                  setInput({ ...input, on_leave: event.target.checked })
                }
              />
              İzinde
            </label>
          </div>
          <p className="small-text muted">
            İzin, vardiya ve beceri değişiklikleri kaydedildiğinde uygun olmayan
            açık atamalar devredilir. Uygun uzman yoksa kuyrukta bekler.
          </p>
          <fieldset disabled={busy}>
            <legend>Vardiya</legend>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={allDay}
                onChange={(event) =>
                  setInput({
                    ...input,
                    shift_start_minute: event.target.checked ? 0 : 510,
                    shift_end_minute: event.target.checked ? 1440 : 1080,
                  })
                }
              />
              Tüm gün
            </label>
            {!allDay && (
              <div className="form-columns">
                <label>
                  Başlangıç
                  <input
                    type="time"
                    value={timeValue(input.shift_start_minute)}
                    onChange={(event) =>
                      setInput({
                        ...input,
                        shift_start_minute: timeToMinutes(event.target.value),
                      })
                    }
                  />
                </label>
                <label>
                  Bitiş
                  <input
                    type="time"
                    value={timeValue(input.shift_end_minute)}
                    onChange={(event) => {
                      const minute = timeToMinutes(event.target.value);
                      setInput({
                        ...input,
                        shift_end_minute: minute === 0 ? 1440 : minute,
                      });
                    }}
                  />
                </label>
              </div>
            )}
            <div className="weekday-options">
              {['Pzt', 'Sal', 'Çar', 'Per', 'Cum', 'Cmt', 'Paz'].map(
                (label, index) => (
                  <label className="checkbox-label" key={label}>
                    <input
                      type="checkbox"
                      checked={input.weekdays.includes(index + 1)}
                      onChange={(event) =>
                        setInput({
                          ...input,
                          weekdays: event.target.checked
                            ? [...input.weekdays, index + 1].sort(
                                (a, b) => a - b,
                              )
                            : input.weekdays.filter((day) => day !== index + 1),
                        })
                      }
                    />
                    {label}
                  </label>
                ),
              )}
            </div>
          </fieldset>
          <fieldset disabled={busy}>
            <legend>Beceriler · 1 temel, 5 ileri</legend>
            <div className="skill-options">
              {catalog.map((skill) => (
                <label key={skill.code}>
                  {skill.label}
                  <select
                    value={
                      input.skills.find((item) => item.code === skill.code)
                        ?.level ?? 0
                    }
                    onChange={(event) => {
                      const level = Number(event.target.value);
                      setInput({
                        ...input,
                        skills: [
                          ...input.skills.filter(
                            (item) => item.code !== skill.code,
                          ),
                          ...(level ? [{ code: skill.code, level }] : []),
                        ],
                      });
                    }}
                  >
                    <option value={0}>Yok</option>
                    {[1, 2, 3, 4, 5].map((level) => (
                      <option key={level} value={level}>
                        {level}
                      </option>
                    ))}
                  </select>
                </label>
              ))}
            </div>
          </fieldset>
          {error && (
            <p role="alert" className="failure">
              {error}
            </p>
          )}
          <button
            disabled={
              busy ||
              !Number.isInteger(input.capacity) ||
              input.capacity < 1 ||
              input.capacity > 100 ||
              input.weekdays.length === 0 ||
              !Number.isInteger(input.shift_start_minute) ||
              !Number.isInteger(input.shift_end_minute) ||
              input.shift_start_minute >= input.shift_end_minute
            }
            onClick={save}
          >
            {busy ? 'Devrediliyor…' : 'Uzman ayarlarını kaydet'}
          </button>
        </>
      )}
    </article>
  );
}

export function ExpertBoard({
  user,
  onExpired,
  onChanged,
  onQueue,
}: {
  user: User;
  onExpired: () => void;
  onChanged: () => void;
  onQueue: () => void;
}) {
  const [opened, setOpened] = useState(false);
  const [board, setBoard] = useState<Board | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!opened) return;
    let active = true;
    setLoading(true);
    setError('');
    request<Board>('/assignment/experts')
      .then((result) => {
        if (active) setBoard(result);
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
  }, [opened, attempt, onExpired]);
  return (
    <details
      className="status-card expert-board"
      onToggle={(event) => setOpened(event.currentTarget.open)}
    >
      <summary>Uzmanlar ve otomatik atama</summary>
      {opened && (
        <>
          <p className="small-text muted">
            Uygunluk: tüm beceriler, açık vardiya ve boş kapasite. Puan: %50
            beceri + %30 boş kapasite + %20 dağıtım sırası.
          </p>
          <div className="section-heading">
            <p>
              {board
                ? `${board.queued} talep atama bekliyor`
                : 'Uzmanlar yükleniyor…'}
            </p>
            <button
              className="secondary"
              disabled={loading}
              onClick={() => {
                setNotice('');
                setAttempt((value) => value + 1);
              }}
            >
              Yenile
            </button>
          </div>
          {loading && <p role="status">Yük durumu güncelleniyor…</p>}
          {error && (
            <p role="alert" className="failure">
              {error}
            </p>
          )}
          {notice && (
            <p role="status" className="success">
              {notice}
            </p>
          )}
          {board && (
            <>
              <button className="secondary" onClick={onQueue}>
                Atanmayan talepleri göster
              </button>
              {board.experts.length === 0 && (
                <p className="muted">Henüz uzman profili yok.</p>
              )}
              {board.experts.map((expert) => (
                <ExpertCard
                  key={expert.id}
                  expert={expert}
                  catalog={board.skills}
                  editable={['team_lead', 'admin'].includes(user.role)}
                  onExpired={onExpired}
                  onSaved={(result) => {
                    setBoard(result);
                    setNotice(
                      'Uzman kaydedildi ve açık atamalar yeniden değerlendirildi.',
                    );
                    onChanged();
                  }}
                />
              ))}
            </>
          )}
        </>
      )}
    </details>
  );
}

export function AssignmentHistory({
  id,
  revision,
  onExpired,
}: {
  id: number;
  revision: string;
  onExpired: () => void;
}) {
  const [items, setItems] = useState<History[]>([]);
  const [cursor, setCursor] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    setItems([]);
    setCursor('');
    request<{ items: History[]; next_cursor: string }>(
      `/tickets/${id}/assignments?limit=20`,
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
  }, [id, revision, attempt, onExpired]);
  async function loadMore() {
    setLoading(true);
    setError('');
    try {
      const result = await request<{ items: History[]; next_cursor: string }>(
        `/tickets/${id}/assignments?limit=20&before=${cursor}`,
      );
      setItems((current) => [...current, ...result.items]);
      setCursor(result.next_cursor);
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        onExpired();
        return;
      }
      setError(errorMessage(error));
    } finally {
      setLoading(false);
    }
  }
  const reasons: Record<string, string> = {
    ticket_created: 'Yeni talep',
    expert_leave: 'Uzman izne ayrıldı',
    expert_update: 'Uzman ayarları değişti',
    status_changed: 'Talep durumu değişti',
    capacity_released: 'Kapasite açıldı',
    capacity_available: 'Uygun uzman bulundu',
    availability_check: 'Vardiya ve uygunluk kontrolü',
    manual_retry: 'Yeniden atama denemesi',
  };
  return (
    <section
      className="assignment-history"
      aria-labelledby="assignment-history-title"
    >
      <h3 id="assignment-history-title">Atama geçmişi</h3>
      {loading && <p role="status">Atamalar yükleniyor…</p>}
      {error && (
        <div role="alert">
          <p className="failure">{error}</p>
          <button
            className="secondary"
            onClick={() => setAttempt((value) => value + 1)}
          >
            Tekrar dene
          </button>
        </div>
      )}
      {!loading && !error && items.length === 0 && (
        <p className="muted">Henüz atama kararı yok.</p>
      )}
      <ol className="assignment-timeline">
        {items.map((item) => (
          <li key={item.id}>
            <strong>
              {item.from_name || 'Atanmamış'} →{' '}
              {item.to_name || 'Atama kuyruğu'}
            </strong>
            <p className="small-text">
              {reasons[item.reason.split(':')[0]] ?? item.reason} ·{' '}
              {item.actor_name} · {dateTime(item.created_at)}
            </p>
            {typeof item.score.total === 'number' && (
              <details>
                <summary>Atama puanı: {item.score.total} / 10000</summary>
                <p className="small-text">
                  Beceri: {(item.score.skill_points ?? 0) / 10}% · Boş kapasite:{' '}
                  {(item.score.spare_points ?? 0) / 10}% · Dağıtım sırası:{' '}
                  {(item.score.rotation_points ?? 0) / 10}%
                </p>
              </details>
            )}
          </li>
        ))}
      </ol>
      {cursor && (
        <button className="secondary" disabled={loading} onClick={loadMore}>
          Daha eski atamalar
        </button>
      )}
    </section>
  );
}

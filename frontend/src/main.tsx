import {
  StrictMode,
  useCallback,
  useEffect,
  useState,
  type FormEvent,
} from 'react';
import { createRoot } from 'react-dom/client';
import { APIError, errorMessage, request, type User } from './api';
import './style.css';
import { Tickets } from './Tickets';
import { Assets } from './Assets';
import { Changes } from './Changes';
import { Knowledge } from './Knowledge';
import { Reports } from './Reports';
import { Simulation } from './Simulation';
import { ThemeMenu } from './ThemeMenu';

type Status = 'loading' | 'ready' | 'error';
const roleLabels: Record<string, string> = {
  employee: 'Çalışan',
  specialist: 'Destek uzmanı',
  team_lead: 'Takım lideri',
  asset_manager: 'Varlık yöneticisi',
  admin: 'Yönetici',
};

function ConnectionStatus() {
  const [status, setStatus] = useState<Status>('loading');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setStatus('loading');
    request<{ status: string; database: string }>('/health')
      .then((result) => {
        if (active)
          setStatus(
            result.status === 'ok' && result.database === 'ok'
              ? 'ready'
              : 'error',
          );
      })
      .catch(() => {
        if (active) setStatus('error');
      });
    return () => {
      active = false;
    };
  }, [attempt]);
  return (
    <section className="status-card compact" aria-labelledby="status-title">
      <div className="card-heading">
        <span className={`indicator ${status}`} aria-hidden="true" />
        <h2 id="status-title">Servis bağlantısı</h2>
      </div>
      <p role="status">
        {status === 'loading'
          ? 'Servisler kontrol ediliyor…'
          : status === 'ready'
            ? 'Uygulama ve veritabanı yanıt veriyor.'
            : 'Bağlantı kurulamadı. Tekrar deneyin.'}
      </p>
      <button
        className="secondary"
        disabled={status === 'loading'}
        onClick={() => setAttempt((value) => value + 1)}
      >
        Yeniden kontrol et
      </button>
    </section>
  );
}

function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [email, setEmail] = useState('calisan@destekmasa.local');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      const result = await request<{ user: User }>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      });
      setPassword('');
      onLogin(result.user);
    } catch (error) {
      setError(errorMessage(error, 'Giriş yapılamadı.'));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="status-card" aria-labelledby="login-title">
      <p className="eyebrow">HESABINIZ</p>
      <h2 id="login-title">DestekMasa’ya giriş yapın</h2>
      <p className="muted">
        Destek taleplerinizi takip etmek için hesabınıza giriş yapın.
      </p>
      <form onSubmit={submit}>
        <label htmlFor="email">E-posta</label>
        <input
          id="email"
          type="email"
          autoComplete="username"
          required
          maxLength={254}
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          disabled={busy}
        />
        <label htmlFor="password">Parola</label>
        <input
          id="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          disabled={busy}
        />
        {error && (
          <p className="failure" role="alert">
            {error}
          </p>
        )}
        <button className="full-width" disabled={busy} type="submit">
          {busy ? 'Giriş yapılıyor…' : 'Giriş yap'}
        </button>
      </form>
    </section>
  );
}

function Account({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function logout() {
    setBusy(true);
    setError('');
    try {
      await request<void>('/auth/logout', { method: 'POST' });
      onLogout();
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        onLogout();
        return;
      }
      setError(errorMessage(error, 'Çıkış yapılamadı.'));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="status-card" aria-labelledby="account-title">
      <div className="card-heading">
        <span className="badge">{roleLabels[user.role] ?? user.role}</span>
      </div>
      <h2 className="account-title" id="account-title">
        Merhaba, {user.display_name}
      </h2>
      <p className="account-email">{user.email}</p>

      {error && (
        <p className="failure" role="alert">
          {error}
        </p>
      )}
      <button className="secondary logout" disabled={busy} onClick={logout}>
        {busy ? 'İşlem yapılıyor…' : 'Çıkış yap'}
      </button>
    </section>
  );
}

type View =
  | 'tickets'
  | 'assets'
  | 'changes'
  | 'knowledge'
  | 'reports'
  | 'simulation';
const inventoryRoles = ['specialist', 'team_lead', 'asset_manager', 'admin'];
const managerRoles = ['team_lead', 'admin'];
const modules: { id: View; label: string; roles?: string[] }[] = [
  { id: 'tickets', label: 'Talepler' },
  { id: 'knowledge', label: 'Bilgi bankası' },
  { id: 'assets', label: 'Varlıklar', roles: inventoryRoles },
  { id: 'changes', label: 'Değişiklikler', roles: inventoryRoles },
  { id: 'reports', label: 'Raporlar', roles: managerRoles },
  { id: 'simulation', label: 'Simülasyon', roles: managerRoles },
];

function ModuleContent({
  view,
  user,
  onExpired,
}: {
  view: View;
  user: User;
  onExpired: () => void;
}) {
  switch (view) {
    case 'simulation':
      return <Simulation onExpired={onExpired} />;
    case 'reports':
      return <Reports onExpired={onExpired} />;
    case 'knowledge':
      return <Knowledge user={user} onExpired={onExpired} />;
    case 'changes':
      return <Changes user={user} onExpired={onExpired} />;
    case 'assets':
      return <Assets user={user} onExpired={onExpired} />;
    case 'tickets':
      return <Tickets user={user} onExpired={onExpired} />;
  }
}

function App() {
  const [view, setView] = useState<View>('tickets');
  const [user, setUser] = useState<User | null>(null);
  const [session, setSession] = useState<Status>('loading');
  const [attempt, setAttempt] = useState(0);
  const expireSession = useCallback(() => {
    setUser(null);
    setView('tickets');
    setSession('ready');
  }, []);
  useEffect(() => {
    let active = true;
    setSession('loading');
    request<{ user: User }>('/auth/me')
      .then((result) => {
        if (active) {
          setUser(result.user);
          setSession('ready');
        }
      })
      .catch((error) => {
        if (!active) return;
        setUser(null);
        setSession(
          error instanceof APIError && error.status === 401 ? 'ready' : 'error',
        );
      });
    return () => {
      active = false;
    };
  }, [attempt]);
  return (
    <div className="app-shell">
      <header>
        <a className="brand" href="/">
          D<span>DestekMasa</span>
        </a>
        <div className="header-actions">
          <span className="header-label">BT yardım masası</span>
          <ThemeMenu />
        </div>
      </header>
      <main>
        <p className="eyebrow">BT DESTEK PORTALINIZ</p>
        <h1>Birlikte çözüme ulaşalım.</h1>
        <p className="intro">
          Kurumunuzun destek süreçleri için tek bir başlangıç noktası.
        </p>
        {session === 'loading' && (
          <section className="status-card">
            <p role="status">Oturum kontrol ediliyor…</p>
          </section>
        )}
        {session === 'error' && (
          <section className="status-card">
            <p role="alert">Oturum bilgisi alınamadı. Lütfen tekrar deneyin.</p>
            <button onClick={() => setAttempt((value) => value + 1)}>
              Tekrar dene
            </button>
          </section>
        )}
        {session === 'ready' &&
          (user ? (
            <>
              <Account user={user} onLogout={expireSession} />
              <nav className="module-nav" aria-label="Modüller">
                {modules
                  .filter(
                    (module) =>
                      !module.roles || module.roles.includes(user.role),
                  )
                  .map((module) => (
                    <button
                      key={module.id}
                      className={view === module.id ? '' : 'secondary'}
                      aria-current={view === module.id ? 'page' : undefined}
                      onClick={() => setView(module.id)}
                    >
                      {module.label}
                    </button>
                  ))}
              </nav>
              <ModuleContent
                key={user.id}
                view={view}
                user={user}
                onExpired={expireSession}
              />
            </>
          ) : (
            <Login onLogin={setUser} />
          ))}
        <ConnectionStatus />
      </main>
      <footer>
        DestekMasa <span>Mehmet İnan · DDO projesi</span>
      </footer>
    </div>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

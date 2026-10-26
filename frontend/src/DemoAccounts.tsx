const accounts = [
  ['Çalışan', 'calisan'],
  ['Destek uzmanı', 'uzman'],
  ['Destek uzmanı 2', 'uzman2'],
  ['Destek uzmanı 3', 'uzman3'],
  ['Takım lideri', 'lider'],
  ['Varlık yöneticisi', 'varlik'],
  ['Yönetici', 'admin'],
] as const;

export function DemoAccounts({
  busy,
  onSelect,
}: {
  busy: boolean;
  onSelect: (email: string, password: string) => void;
}) {
  const password = import.meta.env.VITE_DEMO_PASSWORD;
  if (import.meta.env.VITE_DEMO_ENABLED !== 'true' || !password) return null;

  return (
    <section className="demo-accounts" aria-labelledby="demo-title">
      <h3 id="demo-title">Demo hesapları</h3>
      <p>
        Bir hesap seçerek giriş bilgilerini doldurun, ardından Giriş yap’a
        basın.
      </p>
      <p>
        Ortak demo parolası: <code>{password}</code>
      </p>
      <div className="demo-account-list">
        {accounts.map(([role, name]) => {
          const email = `${name}@destekmasa.local`;
          return (
            <button
              key={name}
              type="button"
              className="secondary demo-account"
              disabled={busy}
              onClick={() => onSelect(email, password)}
            >
              <strong>{role}</strong>
              <span>{email}</span>
            </button>
          );
        })}
      </div>
    </section>
  );
}

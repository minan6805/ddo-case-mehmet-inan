import { useEffect, useState } from 'react';

type ThemePreference = 'system' | 'light' | 'dark';

declare global {
  interface Window {
    destekmasaTheme: {
      preference(): ThemePreference;
      setPreference(value: ThemePreference): void;
    };
  }
}

export function ThemeMenu() {
  const [preference, setPreference] = useState(
    window.destekmasaTheme.preference,
  );
  useEffect(() => {
    const sync = () => setPreference(window.destekmasaTheme.preference());
    window.addEventListener('destekmasa-theme-change', sync);
    return () => window.removeEventListener('destekmasa-theme-change', sync);
  }, []);

  return (
    <label className="theme-menu">
      <span>Tema</span>
      <select
        aria-label="Görünüm teması"
        value={preference}
        onChange={(event) =>
          window.destekmasaTheme.setPreference(
            event.target.value as ThemePreference,
          )
        }
      >
        <option value="system">Sistem</option>
        <option value="light">Açık</option>
        <option value="dark">Koyu</option>
      </select>
    </label>
  );
}

// Apply the saved preference before the application renders.
(() => {
  const key = 'destekmasa.theme';
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  const valid = (value) => ['light', 'dark', 'system'].includes(value);
  let preference = 'system';
  try {
    const saved = localStorage.getItem(key);
    if (valid(saved)) preference = saved;
  } catch {
    // The theme still works when browser storage is unavailable.
  }

  function apply() {
    const theme =
      preference === 'system'
        ? system.matches
          ? 'dark'
          : 'light'
        : preference;
    document.documentElement.dataset.theme = theme;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute('content', theme === 'dark' ? '#101923' : '#f5f7fa');
    window.dispatchEvent(new Event('destekmasa-theme-change'));
  }

  window.destekmasaTheme = {
    preference: () => preference,
    setPreference(value) {
      if (!valid(value)) return;
      preference = value;
      try {
        if (value === 'system') localStorage.removeItem(key);
        else localStorage.setItem(key, value);
      } catch {
        // Keep the in-memory choice for this visit.
      }
      apply();
    },
  };
  system.addEventListener('change', () => {
    if (preference === 'system') apply();
  });
  window.addEventListener('storage', (event) => {
    if (event.key !== key && event.key !== null) return;
    preference = valid(event.newValue) ? event.newValue : 'system';
    apply();
  });
  apply();
})();

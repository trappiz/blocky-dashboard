'use strict';

// Applies the colour theme before the page is drawn, so it never flashes in
// the wrong one. Loaded without defer from <head>; app.js adds the switch.
// The preference is "light", "dark" or "system" (follow the OS setting).
(() => {
  const media = window.matchMedia('(prefers-color-scheme: dark)');

  function pref() {
    try {
      const v = localStorage.getItem('theme');
      return v === 'light' || v === 'dark' ? v : 'system';
    } catch {
      return 'system';
    }
  }

  function apply() {
    const p = pref();
    document.documentElement.dataset.theme = p === 'system' ? (media.matches ? 'dark' : 'light') : p;
  }

  function set(p) {
    try {
      if (p === 'system') localStorage.removeItem('theme');
      else localStorage.setItem('theme', p);
    } catch {}
    apply();
  }

  media.addEventListener('change', apply);
  apply();
  window.blockyTheme = { pref, set };
})();

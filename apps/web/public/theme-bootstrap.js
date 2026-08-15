(() => {
  const preference = document.cookie
    .split('; ')
    .find((entry) => entry.startsWith('aboutme-theme='))
    ?.split('=')[1];
  const theme = preference === 'light' || preference === 'dark'
    ? preference
    : window.matchMedia('(prefers-color-scheme: dark)').matches
      ? 'dark'
      : 'light';

  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
})();

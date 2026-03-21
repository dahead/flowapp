// Apply theme before first paint to prevent flash
(function () {
  var saved = localStorage.getItem('theme');
  var preferred = saved || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  document.documentElement.setAttribute('data-theme', preferred);
})();

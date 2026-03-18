// common.js — shared UI behaviour for all authenticated pages

// ── Theme ────────────────────────────────────────────────────────────────────
function toggleTheme() {
  var html = document.documentElement;
  var next = html.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
  html.setAttribute('data-theme', next);
  localStorage.setItem('theme', next);
}

// ── User menu ────────────────────────────────────────────────────────────────
function toggleUserMenu() {
  document.getElementById('userMenuDropdown').classList.toggle('open');
}

document.addEventListener('click', function(e) {
  var wrap = document.querySelector('.user-menu-wrap');
  if (wrap && !wrap.contains(e.target)) {
    var dd = document.getElementById('userMenuDropdown');
    if (dd) dd.classList.remove('open');
  }
});

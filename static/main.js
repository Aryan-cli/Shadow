(function () {
  var root = document.documentElement;

  function getTheme() {
    var ls = localStorage.getItem('shadow-theme');
    if (ls) return ls;
    var m = document.cookie.match(/(?:^|;\s*)shadow-theme=([^;]*)/);
    return m ? decodeURIComponent(m[1]) : 'dark';
  }

  function setTheme(theme) {
    localStorage.setItem('shadow-theme', theme);
    document.cookie = 'shadow-theme=' + theme + '; path=/; max-age=31536000; samesite=strict';
    if (theme === 'light') {
      root.setAttribute('data-theme', 'light');
    } else {
      root.removeAttribute('data-theme');
    }
  }

  var saved = getTheme();
  if (saved === 'light') root.setAttribute('data-theme', 'light');

  var btn = document.getElementById('themeToggle');
  if (btn) {
    btn.addEventListener('click', function (e) {
      e.stopPropagation();
      e.preventDefault();
      var isLight = root.getAttribute('data-theme') === 'light';
      setTheme(isLight ? 'dark' : 'light');
    });
  }

  var menuToggle = document.getElementById('menuToggle');
  var mobileMenu = document.getElementById('mobileMenu');
  if (menuToggle && mobileMenu) {
    menuToggle.addEventListener('click', function () {
      var open = mobileMenu.classList.toggle('open');
      menuToggle.setAttribute('aria-expanded', open);
    });
  }

  var userMenu = document.getElementById('userMenu');
  var userPillBtn = document.getElementById('userPillBtn');
  if (userMenu && userPillBtn) {
    userPillBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      var open = userMenu.classList.toggle('open');
      userPillBtn.setAttribute('aria-expanded', open);
    });
    document.addEventListener('click', function (e) {
      if (!userMenu.contains(e.target)) {
        userMenu.classList.remove('open');
        userPillBtn.setAttribute('aria-expanded', 'false');
      }
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        userMenu.classList.remove('open');
        userPillBtn.setAttribute('aria-expanded', 'false');
      }
    });
  }
})();

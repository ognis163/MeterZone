(function () {
  try {
    var saved = localStorage.getItem("indextro-theme");
    var dark;
    if (saved === "dark" || saved === "light") {
      dark = saved === "dark";
    } else {
      dark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
    }
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
  } catch (e) {
    document.documentElement.setAttribute("data-theme", "light");
  }
})();

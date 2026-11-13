// The phone Menu. Loaded in <head> without defer so the "js" class is set before the page
// paints; without it the links show as a plain row instead.
(function () {
  var root = document.documentElement;
  root.classList.add("js");

  function setup() {
    var button = document.querySelector(".menu-button");
    var panel = document.getElementById("mobile-nav");
    if (!button || !panel) return;

    function setOpen(open) {
      button.setAttribute("aria-expanded", open ? "true" : "false");
      button.querySelector(".menu-label").textContent = open ? "Close" : "Menu";
      panel.hidden = !open;
      root.classList.toggle("menu-open", open);
    }

    button.addEventListener("click", function () {
      setOpen(button.getAttribute("aria-expanded") !== "true");
    });
    // Any link closes it, including links to a spot on this page.
    panel.addEventListener("click", function (event) {
      if (event.target.closest("a")) setOpen(false);
    });
    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape" && !panel.hidden) {
        setOpen(false);
        button.focus();
      }
    });
    // Wide screens show the full nav, and a page restored with Back starts closed.
    window.matchMedia("(min-width: 720px)").addEventListener("change", function (event) {
      if (event.matches) setOpen(false);
    });
    window.addEventListener("pageshow", function () { setOpen(false); });
    setOpen(false);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", setup);
  } else {
    setup();
  }
})();

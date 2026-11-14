// The invite page: shows the code from the address (/invite/W57ZVGUJ) and copies it.
(function () {
  function ready() {
    var match = location.pathname.match(/^\/invite\/([A-Za-z0-9-]{4,20})\/?$/);
    var code = match ? match[1].replace(/-/g, "").toUpperCase() : "";
    var box = document.querySelector(".invite-code");
    var missing = document.querySelector(".invite-missing");
    if (!code || !box) return;
    document.getElementById("invite-code").textContent = code.length === 8 ? code.slice(0, 4) + " " + code.slice(4) : code;
    box.hidden = false;
    if (missing) missing.hidden = true;
    var button = document.getElementById("copy-code");
    button.addEventListener("click", function () {
      var done = function () {
        button.textContent = "Copied";
        setTimeout(function () { button.textContent = "Copy code"; }, 1500);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(code).then(done, function () {});
      }
    });
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", ready);
  } else {
    ready();
  }
})();

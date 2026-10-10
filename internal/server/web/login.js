/* 登录页逻辑：校验 Key（/api/verify），通过后写入 localStorage 并跳回来源页 */
(function () {
  "use strict";

  var LS_KEY = "gip_key";

  var form = document.getElementById("loginCard");
  var input = document.getElementById("loginKey");
  var btn = document.getElementById("loginBtn");
  var errBox = document.getElementById("loginError");
  var eyeBtn = document.getElementById("eyeBtn");

  /* ---------- 工具 ---------- */

  // 仅允许站内相对路径作为回跳地址，防开放跳转
  function sanitizeNext(v) {
    if (v && v.charAt(0) === "/" && v.charAt(1) !== "/") return v;
    return "/";
  }

  function showErr(msg) {
    errBox.textContent = msg;
    errBox.classList.remove("hidden");
    form.classList.remove("shake");
    void form.offsetWidth; /* 重置动画，让连续失败也有抖动反馈 */
    form.classList.add("shake");
  }

  function hideErr() { errBox.classList.add("hidden"); }

  function setBusy(b) {
    btn.disabled = b;
    btn.textContent = b ? "验证中…" : "登 录";
    input.disabled = b;
    eyeBtn.disabled = b;
  }

  function verify(key) {
    return fetch("/api/verify", { headers: { "X-API-Key": key } }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        var message = (body && body.message) || "";
        if (res.status === 401) {
          var e = new Error(message || "Key 无效，请检查后重试");
          e.auth = true;
          throw e;
        }
        if (!res.ok || (body && body.success === false)) {
          throw new Error(message || "请求失败 (HTTP " + res.status + ")");
        }
        return body.data;
      });
    });
  }

  function doLogin(key) {
    setBusy(true);
    hideErr();
    verify(key).then(function () {
      try { localStorage.setItem(LS_KEY, key); } catch (e) { /* ignore */ }
      location.replace(sanitizeNext(new URLSearchParams(location.search).get("next")));
    }).catch(function (err) {
      setBusy(false);
      if (err.auth) {
        try { localStorage.removeItem(LS_KEY); } catch (e) { /* ignore */ }
      }
      showErr(err.message || "验证失败，请重试");
      input.focus();
      input.select();
    });
  }

  /* ---------- 事件 ---------- */

  form.addEventListener("submit", function (e) {
    e.preventDefault();
    var v = input.value.trim();
    if (!v) { showErr("请输入访问 Key"); input.focus(); return; }
    doLogin(v);
  });

  eyeBtn.addEventListener("click", function () {
    var show = input.type === "password";
    input.type = show ? "text" : "password";
    eyeBtn.setAttribute("aria-pressed", show ? "true" : "false");
    input.focus();
  });

  /* ---------- 启动 ---------- */

  var params = new URLSearchParams(location.search);

  // URL 携带 ?key=：填入并直接登录，随后从地址栏清除（AI Agent / 分享链接直达）
  var urlKey = (params.get("key") || "").trim();
  if (urlKey) {
    input.value = urlKey;
    params.delete("key");
    var qs = params.toString();
    history.replaceState(null, "", location.pathname + (qs ? "?" + qs : "") + location.hash);
    doLogin(urlKey);
    return;
  }

  // 已保存过 Key：先静默校验，有效直接进入主页；失效则清除并停留在登录页
  var saved = "";
  try { saved = localStorage.getItem(LS_KEY) || ""; } catch (e) { /* ignore */ }
  if (saved) {
    input.value = saved;
    doLogin(saved);
  }
})();

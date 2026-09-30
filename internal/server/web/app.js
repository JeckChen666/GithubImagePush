/* GithubImagePush 前端逻辑：key 鉴权、上传、图库管理 */
(function () {
  "use strict";

  var LS_KEY = "gip_key";
  var state = {
    key: localStorage.getItem(LS_KEY) || "",
    config: null,        // /api/verify 返回的配置摘要
    galleryDir: "",      // 当前图库目录（仓库相对路径）
    galleryData: [],     // 当前目录条目
    modalEntry: null,    // 弹层当前条目
  };

  /* ---------- 基础工具 ---------- */

  function $(sel) { return document.querySelector(sel); }
  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    attrs = attrs || {};
    Object.keys(attrs).forEach(function (k) {
      if (k === "class") node.className = attrs[k];
      else if (k === "text") node.textContent = attrs[k];
      else if (k === "html") node.innerHTML = attrs[k];
      else if (k.startsWith("on")) node.addEventListener(k.slice(2), attrs[k]);
      else node.setAttribute(k, attrs[k]);
    });
    (children || []).forEach(function (c) { node.appendChild(c); });
    return node;
  }

  function humanSize(n) {
    if (n == null) return "";
    if (n < 1024) return n + " B";
    var units = ["KB", "MB", "GB"], i = -1;
    do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
    return n.toFixed(1) + " " + units[i];
  }

  function toast(msg, type) {
    var t = el("div", { class: "toast " + (type || ""), text: msg });
    $("#toastWrap").appendChild(t);
    setTimeout(function () { t.remove(); }, 3200);
  }

  function copyText(text) {
    function done() { toast("已复制到剪贴板", "ok"); }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { legacyCopy(text, done); });
    } else {
      legacyCopy(text, done);
    }
  }
  function legacyCopy(text, done) {
    var ta = el("textarea", { style: "position:fixed;opacity:0" });
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand("copy"); done(); } catch (e) { toast("复制失败，请手动复制", "err"); }
    ta.remove();
  }

  /* ---------- API ---------- */

  function api(path, opts) {
    opts = opts || {};
    opts.headers = Object.assign({ "X-API-Key": state.key }, opts.headers || {});
    if (opts.json) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(opts.json);
      delete opts.json;
    }
    return fetch(path, opts).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (res.status === 401) { setAuthed(false); throw new Error(body.message || "Key 无效或已过期，请重新输入"); }
        if (!res.ok || body.success === false) { throw new Error(body.message || ("请求失败 (HTTP " + res.status + ")")); }
        return body.data;
      });
    });
  }

  /* ---------- 鉴权 ---------- */

  function setAuthed(ok, info) {
    $("#authForm").classList.toggle("hidden", ok);
    $("#authOk").classList.toggle("hidden", !ok);
    if (ok) {
      $("#authChip").textContent = "已鉴权 · " + (info || "");
    }
  }

  function loadConfig() {
    return api("/api/verify").then(function (data) {
      state.config = data;
      setAuthed(true, data.repo ? data.repo + "@" + data.branch : "未配置仓库");
      var alerts = [];
      if (!data.configured) alerts.push("服务端尚未配置 GitHub token / repo，上传与图库不可用（修改 config.yaml 后重启服务）。");
      if (alerts.length) {
        $("#cfgAlert").textContent = alerts.join(" ");
        $("#cfgAlert").classList.remove("hidden");
      } else {
        $("#cfgAlert").classList.add("hidden");
      }
      var exts = (data.allowExtensions || []).join(" / ");
      $("#dzHint").textContent = "支持 " + exts + " · 单文件 ≤ " + humanSize(data.maxUploadSize) + " · 多选上传";
      // 图库初始目录为配置的根路径
      state.galleryDir = data.path || "";
      loadGallery();
    });
  }

  function tryAuth(silent) {
    if (!state.key) { setAuthed(false); return; }
    loadConfig().catch(function (err) {
      setAuthed(false);
      if (!silent) toast(err.message, "err");
    });
  }

  $("#saveKeyBtn").addEventListener("click", function () {
    var v = $("#keyInput").value.trim();
    if (!v) { toast("请输入 Key", "err"); return; }
    state.key = v;
    localStorage.setItem(LS_KEY, v);
    tryAuth(false);
  });
  $("#keyInput").addEventListener("keydown", function (e) { if (e.key === "Enter") $("#saveKeyBtn").click(); });
  $("#logoutBtn").addEventListener("click", function () {
    state.key = "";
    localStorage.removeItem(LS_KEY);
    setAuthed(false);
    toast("已清除本地 Key");
  });

  /* ---------- Tab 切换 ---------- */

  document.querySelectorAll(".tab").forEach(function (tab) {
    tab.addEventListener("click", function () {
      document.querySelectorAll(".tab").forEach(function (t) { t.classList.toggle("active", t === tab); });
      document.querySelectorAll(".tabpanel").forEach(function (p) { p.classList.add("hidden"); });
      $("#tab-" + tab.dataset.tab).classList.remove("hidden");
      if (tab.dataset.tab === "gallery") loadGallery();
    });
  });

  /* ---------- 上传 ---------- */

  var dropzone = $("#dropzone");
  var fileInput = $("#fileInput");

  dropzone.addEventListener("click", function () { fileInput.click(); });
  dropzone.addEventListener("keydown", function (e) { if (e.key === "Enter" || e.key === " ") fileInput.click(); });
  fileInput.addEventListener("change", function () {
    enqueueFiles(Array.prototype.slice.call(fileInput.files));
    fileInput.value = "";
  });

  ["dragenter", "dragover"].forEach(function (ev) {
    dropzone.addEventListener(ev, function (e) { e.preventDefault(); dropzone.classList.add("dragover"); });
  });
  ["dragleave", "drop"].forEach(function (ev) {
    dropzone.addEventListener(ev, function (e) { e.preventDefault(); dropzone.classList.remove("dragover"); });
  });
  dropzone.addEventListener("drop", function (e) {
    enqueueFiles(Array.prototype.slice.call(e.dataTransfer.files));
  });

  // Ctrl+V 粘贴截图上传
  document.addEventListener("paste", function (e) {
    var files = Array.prototype.slice.call(e.clipboardData.files || []);
    if (files.length && $("#tab-upload") && !$("#tab-upload").classList.contains("hidden")) {
      enqueueFiles(files);
    }
  });

  var queue = [];
  var uploading = false;

  function enqueueFiles(files) {
    if (!state.key) { toast("请先输入访问 Key", "err"); return; }
    var imgs = files.filter(function (f) { return f.type.startsWith("image/") || /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif)$/i.test(f.name); });
    if (!imgs.length) { toast("没有可上传的图片文件", "err"); return; }
    imgs.forEach(function (f) {
      var item = renderUploadItem(f);
      queue.push({ file: f, row: item });
    });
    pump();
  }

  function pump() {
    if (uploading) return;
    var next = queue.shift();
    if (!next) return;
    uploading = true;
    uploadFile(next.file, next.row).then(function () {
      uploading = false;
      pump();
    });
  }

  function renderUploadItem(file) {
    var preview = file.type.startsWith("image/")
      ? el("img", { src: URL.createObjectURL(file), alt: "" })
      : el("span", { class: "muted", text: file.type || "image" });
    var thumb = el("div", { class: "thumb" }, [preview]);
    var status = el("span", { class: "status uploading", text: "上传中" });
    var head = el("div", { class: "file-head" }, [
      el("span", { class: "file-name", text: file.name }),
      status,
    ]);
    var meta = el("div", { class: "file-meta", text: humanSize(file.size) });
    var progress = el("div", { class: "progress" }, [el("div")]);
    var body = el("div", { class: "body" }, [head, meta, progress]);
    var row = el("div", { class: "card upload-item" }, [thumb, body]);
    $("#uploadList").prepend(row);
    return { row: row, status: status, progress: progress, meta: meta, body: body };
  }

  function uploadFile(file, ui) {
    return new Promise(function (resolve) {
      var xhr = new XMLHttpRequest();
      xhr.open("POST", "/api/upload");
      xhr.setRequestHeader("X-API-Key", state.key);
      xhr.upload.addEventListener("progress", function (e) {
        if (e.lengthComputable) {
          ui.progress.firstChild.style.width = Math.round((e.loaded / e.total) * 100) + "%";
        }
      });
      xhr.addEventListener("load", function () {
        ui.progress.remove();
        var body = {};
        try { body = JSON.parse(xhr.responseText); } catch (e) { /* ignore */ }
        var item = body.data && body.data[0];
        if (xhr.status === 401) {
          setAuthed(false);
          fail(ui, "Key 无效或已过期，请重新输入");
          return resolve();
        }
        if (item && item.success) {
          ui.status.className = "status ok";
          ui.status.textContent = "已上传";
          ui.meta.textContent = humanSize(item.size) + " · " + item.path;
          renderLinkRow(ui.body, item);
          toast("上传成功：" + item.name, "ok");
        } else {
          fail(ui, (item && item.message) || body.message || ("上传失败 (HTTP " + xhr.status + ")"));
        }
        resolve();
      });
      xhr.addEventListener("error", function () {
        ui.progress.remove();
        fail(ui, "网络错误，上传失败");
        resolve();
      });
      var fd = new FormData();
      fd.append("file", file, file.name);
      xhr.send(fd);
    });
  }

  function fail(ui, msg) {
    ui.status.className = "status err";
    ui.status.textContent = "失败";
    ui.body.appendChild(el("div", { class: "err-msg", text: msg }));
  }

  function renderLinkRow(container, item) {
    var urls = item.urls || {};
    var formats = state.config ? state.config.formats.filter(function (f) { return urls[f]; }) : Object.keys(urls);
    var select = el("select", {});
    formats.forEach(function (f) {
      var opt = el("option", { value: f, text: formatLabel(f) });
      if (f === (state.config && state.config.urlFormat)) opt.selected = true;
      select.appendChild(opt);
    });
    var input = el("input", { readonly: "readonly", value: urls[select.value] || item.url });
    select.addEventListener("change", function () { input.value = urls[select.value] || ""; });
    var copyBtn = el("button", { class: "btn ghost small", onclick: function () { copyText(input.value); }, text: "复制" });
    var mdBtn = el("button", { class: "btn ghost small", onclick: function () { copyText("![](" + input.value + ")"); }, text: "Markdown" });
    var htmlBtn = el("button", { class: "btn ghost small", onclick: function () { copyText('<img src="' + input.value + '" alt=""/>'); }, text: "HTML" });
    container.appendChild(el("div", { class: "link-row" }, [select, input, copyBtn, mdBtn, htmlBtn]));
  }

  function formatLabel(f) {
    return { raw: "raw 直链", jsdelivr: "jsDelivr CDN", github: "github.com", custom: "自定义域名" }[f] || f;
  }

  /* ---------- 图库 ---------- */

  function loadGallery() {
    if (!state.key || !state.config) return;
    api("/api/list?dir=" + encodeURIComponent(state.galleryDir)).then(function (data) {
      state.galleryData = data.entries || [];
      renderBreadcrumb(data.dir || "");
      renderGallery();
    }).catch(function (err) {
      $("#galleryGrid").innerHTML = "";
      // git 不存在空目录：目录从未上传过或已清空时按空处理
      if (/不存在/.test(err.message)) {
        state.galleryData = [];
        renderBreadcrumb(state.galleryDir);
        renderGallery();
      } else {
        $("#galleryEmpty").textContent = err.message;
        $("#galleryEmpty").classList.remove("hidden");
      }
    });
  }

  $("#refreshBtn").addEventListener("click", loadGallery);

  function renderBreadcrumb(dir) {
    var bc = $("#breadcrumb");
    bc.innerHTML = "";
    var rootName = (state.config && state.config.repo) || "仓库根目录";
    bc.appendChild(el("a", { text: rootName, onclick: function () { state.galleryDir = state.config.path || ""; loadGallery(); } }));
    var acc = "";
    dir.split("/").filter(Boolean).forEach(function (seg) {
      acc = acc ? acc + "/" + seg : seg;
      var path = acc;
      bc.appendChild(el("span", { text: " / " }));
      bc.appendChild(el("a", { text: seg, onclick: function () { state.galleryDir = path; loadGallery(); } }));
    });
    bc.appendChild(el("span", { text: " / ", class: "" }));
    bc.appendChild(el("span", { class: "cur", text: "当前" }));
  }

  function renderGallery() {
    var grid = $("#galleryGrid");
    grid.innerHTML = "";
    var entries = state.galleryData;
    $("#galleryEmpty").classList.toggle("hidden", entries.length > 0);
    entries.sort(function (a, b) { return (b.isDir - a.isDir) || a.name.localeCompare(b.name); });
    entries.forEach(function (entry) {
      grid.appendChild(entry.isDir ? dirCard(entry) : fileCard(entry));
    });
  }

  function dirCard(entry) {
    var icon = el("div", { class: "g-thumb", html: '<svg viewBox="0 0 24 24" width="42" height="42"><path fill="currentColor" d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2Z"/></svg>' });
    return el("div", { class: "card g-item dir", onclick: function () { state.galleryDir = entry.path; loadGallery(); } }, [
      icon,
      el("div", { class: "g-name", text: entry.name + "/" }),
    ]);
  }

  function fileCard(entry) {
    var img = el("img", { loading: "lazy", alt: entry.name, src: entry.url });
    img.addEventListener("error", function () {
      // 直链不可访问（如私有仓库）时回退到服务端代理
      if (!img.dataset.fb) {
        img.dataset.fb = "1";
        img.src = "/api/raw?path=" + encodeURIComponent(entry.path) + "&key=" + encodeURIComponent(state.key);
      }
    });
    var overlay = el("div", { class: "g-overlay" }, [
      el("button", {
        text: "复制", onclick: function (e) { e.stopPropagation(); copyText(entry.url); },
      }),
      el("button", {
        class: "del", text: "删除",
        onclick: function (e) { e.stopPropagation(); confirmDelete(entry); },
      }),
    ]);
    return el("div", { class: "card g-item", onclick: function () { openDetail(entry); } }, [
      el("div", { class: "g-thumb" }, [img]),
      overlay,
      el("div", { class: "g-name", title: entry.name, text: entry.name }),
    ]);
  }

  function confirmDelete(entry) {
    if (!confirm("确认删除 " + entry.path + " ？该操作会在仓库产生一次删除提交，不可恢复。")) return;
    api("/api/delete?path=" + encodeURIComponent(entry.path), { method: "DELETE" }).then(function () {
      toast("已删除：" + entry.name, "ok");
      loadGallery();
    }).catch(function (err) { toast(err.message, "err"); });
  }

  /* ---------- 详情弹层 ---------- */

  var modal = $("#detailModal");
  modal.addEventListener("click", function (e) {
    if (e.target.dataset.close) closeModal();
  });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeModal(); });

  function closeModal() { modal.classList.add("hidden"); state.modalEntry = null; }

  function openDetail(entry) {
    state.modalEntry = entry;
    $("#detailName").textContent = entry.name;
    $("#detailMeta").textContent = humanSize(entry.size) + " · " + entry.path + " · " + (entry.sha || "").slice(0, 8);
    var img = el("img", { alt: entry.name, src: entry.url });
    img.addEventListener("error", function () {
      if (!img.dataset.fb) {
        img.dataset.fb = "1";
        img.src = "/api/raw?path=" + encodeURIComponent(entry.path) + "&key=" + encodeURIComponent(state.key);
      }
    });
    var preview = $("#detailPreview");
    preview.innerHTML = "";
    preview.appendChild(img);

    var links = $("#detailLinks");
    links.innerHTML = "";
    ["markdown", "html", "bbcode"].forEach(function (fmt) {
      links.appendChild(el("div", { class: "link-row-item" }, [
        el("label", { text: fmt.toUpperCase() }),
        el("input", { readonly: "readonly", value: fmt === "markdown" ? "![](" + entry.url + ")" : fmt === "html" ? '<img src="' + entry.url + '" alt=""/>' : "[img]" + entry.url + "[/img]" }),
        el("button", { class: "btn ghost small", text: "复制", onclick: function (ev) { copyText(ev.target.previousElementSibling.value); } }),
      ]));
    });
    Object.keys(entry.urls || {}).forEach(function (fmt) {
      links.appendChild(el("div", { class: "link-row-item" }, [
        el("label", { text: formatLabel(fmt) }),
        el("input", { readonly: "readonly", value: entry.urls[fmt] }),
        el("button", { class: "btn ghost small", text: "复制", onclick: function (ev) { copyText(ev.target.previousElementSibling.value); } }),
      ]));
    });

    $("#detailOpen").href = "https://github.com/" + (state.config ? state.config.repo : "") +
      "/blob/" + (state.config ? state.config.branch : "main") + "/" + entry.path;
    $("#detailDelete").onclick = function () { closeModal(); confirmDelete(entry); };
    modal.classList.remove("hidden");
  }

  /* ---------- 启动 ---------- */

  tryAuth(true);
})();

(function () {
  "use strict";

  var qa = function (sel, root) {
    return Array.prototype.slice.call((root || document).querySelectorAll(sel));
  };
  var q = function (sel, root) {
    return (root || document).querySelector(sel);
  };

  // Переход на страницу возможен в пределах одного документа или одного
  // сеанса браузера: судим по адресу, а заодно по метке времени.
  var monthScrollStorage = "meterzone-month-pick";
  var monthScrollWindow = 60000;
  var smooth = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches ? false : true;

  function parseVal(raw) {
    if (raw == null) return null;
    var s = String(raw)
      .replace(/\u00a0/g, "")
      .replace(/\s/g, "")
      .replace(",", ".");
    if (!s) return null;
    var n = Number(s);
    return isFinite(n) ? n : null;
  }

  function round3(v) { return Math.round(v * 1000) / 1000; }
  function round2(v) { return Math.round(v * 100) / 100; }

  function fmt(v) {
    var r = round3(v);
    var s = r.toFixed(3).replace(/\.?0+$/, "");
    return s.replace(".", ",");
  }

  var moneyFmt = new Intl.NumberFormat("ru-RU", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });

  function fmtMoney(v) {
    return moneyFmt.format(round2(v)) + " ₽";
  }
  function recalcReadings() {
    var inputs = qa(".meter-input input");
    if (!inputs.length) return;
    var own = {};
    inputs.forEach(function (input) {
      var prev = parseVal(input.getAttribute("data-prev"));
      var val = parseVal(input.value);
      var id = input.getAttribute("data-meter");
      var d = null;
      if (val != null && prev != null && val >= prev) {
        d = round3(val - prev);
      }
      own[id] = d;
      var out = q(".meter-diff [data-diff='" + id + "']");
      if (out) out.textContent = d == null ? "—" : fmt(d);
    });
    qa("[data-net-parent]").forEach(function (span) {
      var id = span.getAttribute("data-net-parent");
      var value = own[id];
      if (value == null) {
        span.textContent = "—";
        return;
      }
      inputs.forEach(function (input) {
        if (input.getAttribute("data-parent-meter") === id) {
          var c = own[input.getAttribute("data-meter")];
          if (c != null) value -= c;
        }
      });
      span.textContent = value >= 0 ? fmt(value) : "—";
    });
    recalcTotals();
  }

  // Итоги месяца на странице: пересчитываем по введённым значениям.
  function recalcTotals() {
    var water = 0, energy = 0, hasWater = false, hasEnergy = false;
    qa(".section").forEach(function (section) {
      var unit = q(".unit-label", section);
      var isWater = unit && unit.textContent.indexOf("куб") === 0;
      qa(".meter", section).forEach(function (row) {
        if (row.classList.contains("is-sub")) return;
        var input = q(".meter-input input", row);
        if (!input) return;
        var prev = parseVal(input.getAttribute("data-prev"));
        var val = parseVal(input.value);
        if (prev == null || val == null || val < prev) return;
        var d = round3(val - prev);
        if (isWater) { water = round3(water + d); hasWater = true; }
        else { energy = round3(energy + d); hasEnergy = true; }
      });
    });
    var w = q("[data-water-total]");
    if (w) w.textContent = hasWater ? fmt(water) : "0";
    var e = q("[data-energy-total]");
    if (e) e.textContent = hasEnergy ? fmt(energy) : "0";
  }

  // ---------- автосохранение ----------
  var entryForm = q("form[data-entry]");
  // Статус один и показывается в шапке; ввода показаний это тоже касается.
  var statusBox = q("[data-save-status]");
  var pending = {}; // key -> функция отправки, выполняется по таймеру
  var saveSeq = 0;
  var inflight = 0; // сколько запросов сохранения сейчас в пути

  function track(promise) {
    inflight++;
    var done = function () { inflight = Math.max(0, inflight - 1); };
    promise.then(done, done);
    return promise;
  }

  // Сообщение в шапке живёт несколько секунд и исчезает: постоянной
  // надписи «Показания сохраняются» больше нет.
  var statusTimer = 0;
  function setStatus(text, kind) {
    if (!statusBox) return;
    statusBox.textContent = text || "";
    statusBox.setAttribute("data-state", kind || "");
    statusBox.hidden = !text;
    clearTimeout(statusTimer);
    if (!text) return;
    statusTimer = setTimeout(function () {
      statusBox.hidden = true;
      statusBox.textContent = "";
    }, 4000);
  }

  function markSaving() {
    saveSeq++;
    setStatus("Сохранение…", "saving");
  }

  function markSaved() {
    saveSeq = Math.max(0, saveSeq - 1);
    if (saveSeq > 0) return;
    var d = new Date();
    var hh = String(d.getHours()).padStart(2, "0");
    var mm = String(d.getMinutes()).padStart(2, "0");
    setStatus("Сохранено " + hh + ":" + mm, "ok");
  }

  function markFailed(msg) {
    saveSeq = Math.max(0, saveSeq - 1);
    setStatus("Не сохранено: " + (msg || "ошибка"), "error");
  }

  function send(url, bodyOf) {
    markSaving();
    return track(fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "Accept": "application/json" },
      credentials: "same-origin",
      body: bodyOf(),
    }).then(function (resp) {
      return resp.json().catch(function () { return {}; }).then(function (data) {
        if (!resp.ok) throw new Error(data.error || "ошибка сохранения");
        markSaved();
        return data;
      });
    }).catch(function (err) {
      markFailed(err.message);
      throw err;
    }));
  }

  function flushPending() {
    Object.keys(pending).forEach(function (key) {
      clearTimeout(pending[key]);
      var fn = pending[key];
      delete pending[key];
      if (typeof fn === "function") fn();
    });
  }

  // Перед PDF или печатью дожидаемся, пока автосохранение дошлёт данные.
  function waitForSaves() {
    flushPending();
    return new Promise(function (resolve) {
      var tries = 0;
      (function wait() {
        if (inflight <= 0 || tries++ > 50) {
          resolve();
          return;
        }
        setTimeout(wait, 100);
      })();
    });
  }

  function formField(name, value) {
    return encodeURIComponent(name) + "=" + encodeURIComponent(value);
  }

  if (entryForm) {
    var objectID = entryForm.getAttribute("data-object");
    var period = entryForm.getAttribute("data-period");
    var baseURL = "/o/" + encodeURIComponent(objectID) + "/save?m=" + encodeURIComponent(period);
    var moneyURL = "/o/" + encodeURIComponent(objectID) + "/money?m=" + encodeURIComponent(period);

    function meterKey(input) {
      return "v:" + input.getAttribute("data-meter");
    }

    function validateMeter(input) {
      var wrap = input.closest(".meter");
      var errEl = wrap ? q(".meter-error", wrap) : null;
      var prev = parseVal(input.getAttribute("data-prev"));
      var val = parseVal(input.value);
      var bad = false;
      if (wrap) wrap.classList.remove("is-invalid");
      if (errEl) errEl.remove();
      if (input.value.trim() !== "" && prev != null && val != null && val < prev) {
        bad = true;
        if (wrap) {
          wrap.classList.add("is-invalid");
          var nameEl = q(".meter-name", wrap);
          if (nameEl) {
            nameEl.insertAdjacentHTML("afterend", '<p class="meter-error">Меньше предыдущего — проверьте число.</p>');
          }
        }
      }
      return bad;
    }

    // Проверяем значение без перерисовки сообщения — для подсветки на лету.
    function invalidMeter(input) {
      var prev = parseVal(input.getAttribute("data-prev"));
      var val = parseVal(input.value);
      return input.value.trim() !== "" && prev != null && val != null && val < prev;
    }

    qa(".meter-input input", entryForm).forEach(function (input) {
      input.addEventListener("input", function () {
        validateMeter(input);
        if (invalidMeter(input)) return;
        var key = meterKey(input);
        clearTimeout(pending[key]);
        pending[key] = setTimeout(function () {
          delete pending[key];
          if (validateMeter(input)) return;
          send(baseURL, function () { return bodyForMeter(input); }).catch(function () {});
        }, 700);
      });
      input.addEventListener("change", function () {
        var key = meterKey(input);
        clearTimeout(pending[key]);
        delete pending[key];
        validateMeter(input);
        if (invalidMeter(input)) return;
        send(baseURL, function () { return bodyForMeter(input); }).catch(function () {});
      });
    });

    function bodyForMeter(input) {
      return [formField("meter", input.getAttribute("data-meter")), formField("value", input.value.trim())].join("&");
    }

    // Финансы: автосохранение по уходу из поля и по кнопке-галочке.
    // Отдельного индикатора нет — статус в шапке общий для всех полей.
    function sendMoney(acc, field, value) {
      markSaving();
      return track(fetch(moneyURL, {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
          "Accept": "application/json"
        },
        credentials: "same-origin",
        body: "account=" + encodeURIComponent(acc) + "&field=" + field + "&value=" + encodeURIComponent(value)
      }).then(function (resp) {
        return resp.json().catch(function () { return {}; }).then(function (data) {
          if (!resp.ok) throw new Error(data.error || "ошибка сохранения");
          markSaved();
          return data;
        });
      }).catch(function (err) {
        markFailed(err.message);
        throw err;
      }));
    }

    qa("[data-money-row]").forEach(function (row) {
      var acc = row.getAttribute("data-account");
      qa("input[data-charged], input[data-advance]", row).forEach(function (input) {
        var field = input.hasAttribute("data-charged") ? "charged" : "advance";
        input.addEventListener("change", function () {
          var raw = input.value.trim();
          if (raw !== "" && parseVal(raw) == null) {
            markFailed("введите число");
            return;
          }
          sendMoney(acc, field, raw).catch(function () {});
        });
      });
    });

    qa("[data-paid-toggle]").forEach(function (toggle) {
      toggle.addEventListener("click", function () {
        var now = !toggle.classList.contains("is-paid");
        var target = toggle.getAttribute("data-flag-target");
        var hidden = target ? q('[data-paid-input="' + target + '"]') : null;
        toggle.classList.toggle("is-paid", now);
        toggle.classList.toggle("is-unpaid", !now);
        toggle.textContent = "✓";
        toggle.title = now ? "Оплачено" : "Отметить оплату";
        toggle.setAttribute("aria-pressed", now ? "true" : "false");
        if (hidden) hidden.value = now ? "1" : "";
        var row = toggle.closest("[data-money-row]");
        var acc = row ? row.getAttribute("data-account") : "";
        var field = target && target.indexOf("advance_paid_") === 0 ? "advance_paid" : "paid";
        sendMoney(acc, field, now ? "1" : "").catch(function () {});
      });
    });

    // Пустая строка вместо нуля при фокусе в поле денег.
    qa(".money-field input").forEach(function (input) {
      input.addEventListener("focus", function () {
        if (input.value.trim() === "0" || input.value.trim() === "0,00") input.value = "";
      });
    });

    // Уход со страницы: досылаем несохранённое.
    window.addEventListener("pagehide", function () { flushPending(); });
  }

  function recalcAll() {
    recalcReadings();
  }

  document.addEventListener("input", function (e) {
    if (!e.target || !e.target.matches) return;
    if (e.target.matches(".meter-input input") || e.target.matches(".money-field input")) {
      recalcAll();
    }
  });

  // Разрешаем цифры, запятую, точку и пробелы; никаких букв и e-нотации.
  qa(".meter-input input, .money-field input").forEach(function (input) {
    input.addEventListener("beforeinput", function (e) {
      if (e.data && /[^0-9.,\s]/.test(e.data)) {
        e.preventDefault();
      }
    });
  });

  recalcAll();

  // Форма работает на автосохранении: Enter не должен перезагружать страницу.
  qa("form[data-entry]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      if (!e.submitter || !e.submitter.hasAttribute("data-allow-submit")) {
        e.preventDefault();
        var next = q(".meter-input input:focus", form);
        if (next) next.blur();
      }
    });
  });

  qa("form[data-confirm]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      if (!window.confirm(form.getAttribute("data-confirm"))) {
        e.preventDefault();
      }
    });
  });

  // Перед отправкой письма дожидаемся автосохранения.
  qa("form[data-send-report]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      if (inflight <= 0 && !Object.keys(pending).length) return;
      e.preventDefault();
      waitForSaves().then(function () { form.submit(); });
    });
  });

  // PDF и печать показывают уже сохранённые показания.
  qa("[data-wait-save]").forEach(function (link) {
    link.addEventListener("click", function (e) {
      if (inflight <= 0 && !Object.keys(pending).length) return;
      e.preventDefault();
      var href = link.getAttribute("href");
      waitForSaves().then(function () { window.location.href = href; });
    });
  });

  qa("[data-print]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      window.print();
    });
  });

  qa("[data-autosubmit]").forEach(function (el) {
    el.addEventListener("change", function () {
      if (el.form) el.form.submit();
    });
  });

  qa("[data-month-pick]").forEach(function (sel) {
    sel.addEventListener("change", function () {
      var hidden = q('input[type="hidden"][name="m"]', sel.form);
      if (hidden) hidden.value = sel.value;
      if (sel.form) sel.form.submit();
    });
  });

  // Выбор месяца на главной: помним, что месяц только что выбран,
  // и после перезагрузки мягко прокручиваем к вводу показаний.
  qa("[data-month-pick]").forEach(function (cell) {
    if (!cell.getAttribute("href")) return;
    cell.addEventListener("click", function () {
      try { window.sessionStorage.setItem(monthScrollStorage, String(Date.now())); } catch (e) {}
    });
  });

  // Браузер может восстановить прежнюю позицию прокрутки — она не нужна:
  // сразу после выбора месяца экран должен стоять на вводе показаний.
  try {
    if (window.sessionStorage.getItem(monthScrollStorage) && "scrollRestoration" in window.history) {
      window.history.scrollRestoration = "manual";
    }
  } catch (e) {}

  (function scrollToEntryAfterMonthPick() {
    var stamp = 0;
    try { stamp = Number(window.sessionStorage.getItem(monthScrollStorage) || 0); } catch (e) {}
    try { window.sessionStorage.removeItem(monthScrollStorage); } catch (e) {}
    if (!stamp || Date.now() - stamp > monthScrollWindow) return;
    var target = q("[data-entry-main]");
    if (!target) return;
    // Даём странице разложиться (шрифты и размеры), затем прокручиваем.
    var done = false;
    function go() {
      if (done) return;
      done = true;
      target.scrollIntoView({ behavior: smooth ? "smooth" : "auto", block: "start" });
    }
    if (document.readyState === "complete") {
      window.requestAnimationFrame(function () { window.requestAnimationFrame(go); });
    } else {
      window.addEventListener("load", go, { once: true });
    }
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(go).catch(function () {});
    setTimeout(go, 400);
  })();

  // Плитки объектов: активный объект показываем в начале ленты.
  qa(".objectrail-scroll").forEach(function (rail) {
    var active = q(".object-tile.is-active", rail);
    if (active) rail.scrollLeft = Math.max(0, active.offsetLeft - 6);
  });

  // Полоса месяцев: выбранный месяц ставим к левому краю, остальные — прокруткой.
  qa("[data-month-strip]").forEach(function (strip) {
    var userScrolled = false;
    function centerSelected() {
      if (userScrolled) return;
      var cells = qa(".month-cell", strip);
      var selected = q(".month-cell.is-selected", strip);
      if (!cells.length || !selected) return;
      // В ленте месяц тянется по содержимому, поэтому ширину задаём явно:
      // тогда все 12 месяцев ложатся в полосу без лишнего запаса, а прокрутка
      // к выбранному месяцу считается по тем же размерам.
      var gap = parseFloat(getComputedStyle(strip).columnGap) || 3;
      var inner = strip.clientWidth;
      if (inner > 0) {
        var cell = Math.floor((inner - 2 - (cells.length - 1) * gap) / cells.length);
        if (cell >= 40) strip.style.setProperty("--cell-w", cell + "px");
      }
      var target = Math.max(0, Math.min(selected.offsetLeft - 2, strip.scrollWidth - strip.clientWidth));
      if (target > 0) strip.scrollLeft = target;
    }
    // До первого кадра браузер ещё не знает ширину ленты — ждём раскладку.
    window.requestAnimationFrame(function () {
      window.requestAnimationFrame(centerSelected);
    });
    // Пользователь сам дёрнул полосу — больше не переставляем.
    ["wheel", "touchstart", "pointerdown", "keydown"].forEach(function (name) {
      strip.addEventListener(name, function () { userScrolled = true; }, { passive: true });
    });
    qa(".month-mobile-arrow", strip.parentNode).forEach(function (arrow) {
      arrow.addEventListener("click", function () { userScrolled = true; });
    });
    window.addEventListener("load", centerSelected);
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(centerSelected).catch(function () {});
    setTimeout(centerSelected, 300);
    strip.addEventListener("wheel", function (e) {
      if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
        e.preventDefault();
        strip.scrollLeft += e.deltaY;
      }
    }, { passive: false });
  });
  // Клик по столбцу диаграммы: подсветить строку журнала и прокрутить к ней.
  qa(".chart-col[data-month]").forEach(function (col) {
    function pick() {
      var m = col.getAttribute("data-month");
      var row = document.querySelector('.jrow[data-period="' + m + '"]');
      if (!row) return;
      qa(".jrow.is-picked").forEach(function (r) { r.classList.remove("is-picked"); });
      qa(".chart-col.is-picked").forEach(function (c) { c.classList.remove("is-picked"); });
      row.classList.add("is-picked");
      col.classList.add("is-picked");
      var top = row.getBoundingClientRect().top + window.scrollY - 78;
      window.scrollTo({ top: Math.max(0, top), behavior: "smooth" });
    }
    col.addEventListener("click", pick);
    col.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        pick();
      }
    });
  });

  // Быстрая подстановка готового адреса из карточки объекта.
  qa("[data-fill-email]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var input = q("#report_email") || q("input[name=email]");
      if (!input) return;
      input.value = btn.getAttribute("data-fill-email");
      input.focus();
      qa(".chip-btn.is-active").forEach(function (b) { b.classList.remove("is-active"); });
      btn.classList.add("is-active");
    });
  });

  // Подстановка адреса при выборе «Вода / Электроэнергия».
  qa("[data-scope-email]").forEach(function (sel) {
    sel.addEventListener("change", function () {
      var opt = sel.options[sel.selectedIndex];
      var email = opt ? opt.getAttribute("data-email") : "";
      var input = q('input[name="email"]', sel.form);
      if (input && email) input.value = email;
    });
  });

  // Печать сразу после открытия предпросмотра (?print=1).
  if (new URLSearchParams(window.location.search).get("print") === "1" && q(".doc-sheet")) {
    setTimeout(function () { window.print(); }, 500);
  }

  // Переключатель «за месяц / за период».
  var kindRadios = qa('input[name="kind"]');
  function syncKind() {
    var current = null;
    kindRadios.forEach(function (r) {
      if (r.checked) current = r.value;
    });
    qa(".period-block").forEach(function (block) {
      block.classList.toggle("is-visible", block.getAttribute("data-period") === current);
    });
  }
  kindRadios.forEach(function (r) {
    r.addEventListener("change", syncKind);
  });
  syncKind();

  // Переключение темы.
  var themeBtn = q("[data-theme-toggle]");
  function applyTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    try { localStorage.setItem("indextro-theme", theme); } catch (e) {}
    var meta = q('meta[name="theme-color"]');
    if (meta) meta.setAttribute("content", theme === "dark" ? "#101312" : "#f6f6f2");
    if (themeBtn) {
      themeBtn.setAttribute("aria-label", theme === "dark" ? "Включить светлую тему" : "Включить тёмную тему");
      themeBtn.setAttribute("title", theme === "dark" ? "Светлая тема" : "Тёмная тема");
    }
  }
  if (themeBtn) {
    applyTheme(document.documentElement.getAttribute("data-theme") || "light");
    themeBtn.addEventListener("click", function () {
      var now = document.documentElement.getAttribute("data-theme") === "dark" ? "light" : "dark";
      applyTheme(now);
    });
  }

  // Сообщение исчезает само.
  var flash = q(".flash");
  if (flash) {
    setTimeout(function () {
      flash.style.transition = "opacity .4s ease";
      flash.style.opacity = "0";
      setTimeout(function () {
        flash.remove();
      }, 450);
    }, 4500);
  }

  // ---------- прибор учёта прямо в карточке объекта ----------
  document.documentElement.setAttribute("data-js", "1");
  (function meterInline() {
      var panel = q("[data-meter-inline]");
      if (!panel) return;
      var form = q("form", panel);
      var idField = q("[data-meter-id]", panel);
      var title = q("[data-meter-inline-title]", panel);
      var saveBtn = q("[data-meter-save]", panel);
      var openBtn = q("[data-meter-open]");
      var defaults = form ? form.innerHTML : "";

      function open(mode, data) {
        panel.classList.add("is-open");
        if (title) title.textContent = mode === "edit" ? "Изменить прибор" : "Новый прибор";
        if (saveBtn) saveBtn.textContent = mode === "edit" ? "Сохранить прибор" : "Добавить прибор";
        if (openBtn) openBtn.style.display = mode === "edit" ? "none" : "";
        if (mode === "edit") {
          if (idField) idField.value = data.id || "0";
          setVal("resource", data.resource);
          setVal("label", data.label);
          setVal("brand", data.brand);
          setVal("serial", data.serial);
          setVal("start_value", data.start_value);
          setVal("installed_on", data.installed_on);
          setVal("parent_id", data.parent_id);
          setVal("finance_mode", data.finance_mode);
          setVal("sort", data.sort);
        }
        var first = q("select, input[type=text]", panel);
        if (first && mode === "edit") first.focus();
        panel.scrollIntoView({ block: "center", behavior: "smooth" });
      }

      function setVal(name, value) {
        var el = q('[name="' + name + '"]', form);
        if (el) el.value = value == null ? "" : String(value);
      }

      function close() {
        panel.classList.remove("is-open");
        if (form) form.innerHTML = defaults;
        form = q("form", panel);
        idField = q("[data-meter-id]", panel);
        saveBtn = q("[data-meter-save]", panel);
        title = q("[data-meter-inline-title]", panel);
        if (openBtn) openBtn.style.display = "";
      }

      if (openBtn) {
        openBtn.addEventListener("click", function () { open("new"); });
      }

      qa("[data-meter-cancel]").forEach(function (btn) {
        btn.addEventListener("click", close);
      });

      qa("[data-meter-edit]").forEach(function (btn) {
        btn.addEventListener("click", function () {
          var id = btn.getAttribute("data-meter-edit");
          var row = btn.closest(".meter-row");
          open("edit", {
            id: id,
            resource: row ? (row.getAttribute("data-resource") || "") : "",
            label: row ? (row.getAttribute("data-label") || "") : "",
            brand: row ? (row.getAttribute("data-brand") || "") : "",
            serial: row ? (row.getAttribute("data-serial") || "") : "",
            start_value: row ? (row.getAttribute("data-start") || "") : "",
            installed_on: row ? (row.getAttribute("data-installed") || "") : "",
            parent_id: row ? (row.getAttribute("data-parent") || "0") : "0",
            finance_mode: row ? (row.getAttribute("data-finance") || "none") : "none",
            sort: row ? (row.getAttribute("data-sort") || "0") : "0"
          });
        });
      });
  })();

  // --------------------------------------------------------------------
  // --------------------------------------------------------------------
  // Начисления в журнале: у каждого счёта своя колонка в «Авансе» и «Счёте».
  // Сумма и отметка оплаты сохраняются сразу, сводка пересчитывается на месте.
  // --------------------------------------------------------------------
  (function () {
    var grids = qa("[data-money-grid]");
    if (!grids.length) return;

    var fmt = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 });
    function money(v) { return fmt.format(round2(v)) + " ₽"; }
    function dash(v) { return round2(v) === 0 ? "—" : money(v); }
    function amount(raw) {
      var v = parseVal(raw);
      return v == null || v < 0 ? 0 : round2(v);
    }

    grids.forEach(function (table) {
      var panel = table.closest("[data-pay-panel]") || table.closest("section");
      var objectID = table.getAttribute("data-object") ||
        (panel ? panel.getAttribute("data-object") : "") || "";
      var paidOut = panel ? panel.querySelector("[data-pay-paid]") : null;
      var dueOut = panel ? panel.querySelector("[data-pay-due]") : null;
      var timers = {};

      function cells(tr) { return qa("td.money-cell", tr); }

      function recalc() {
        var paid = 0, due = 0;
        qa("tbody tr", table).forEach(function (tr) {
          var sum = 0;
          cells(tr).forEach(function (td) {
            var input = q("input.num", td);
            var v = input ? amount(input.value) : 0;
            sum += v;
            if (q(".st.paid", td)) { paid += v; } else { due += v; }
          });
          var out = q("td.total", tr);
          if (out) out.textContent = dash(sum);
        });
        if (paidOut) paidOut.textContent = money(paid);
        if (dueOut) dueOut.textContent = money(due);
      }

      function paint(td) {
        var btn = q(".st", td), input = q("input.num", td);
        if (!btn || !input) return;
        var v = amount(input.value);
        var paid = v > 0 && btn.classList.contains("paid");
        var state = paid ? "paid" : (v === 0 ? "empty" : "due");
        var label = paid ? "Оплачен" : (v === 0 ? "Не задан" : "Не оплачен");
        btn.classList.remove("paid", "due", "empty");
        btn.classList.add(state);
        btn.setAttribute("aria-pressed", paid ? "true" : "false");
        btn.setAttribute("title", label);
        var sr = q(".sr-only", btn);
        if (sr) sr.textContent = label;
        td.classList.toggle("is-unpaid", v > 0 && !paid);
        td.classList.toggle("is-paid", paid);
        input.disabled = paid;
      }

      function save(input, field, value) {
        var url = "/o/" + encodeURIComponent(objectID) + "/money?m=" +
          encodeURIComponent(input.getAttribute("data-p"));
        var body = "account=" + encodeURIComponent(input.getAttribute("data-account")) +
          "&field=" + field + "&value=" + encodeURIComponent(value);
        setStatus("Сохранение…", "saving");
        track(fetch(url, {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
            "Accept": "application/json"
          },
          credentials: "same-origin",
          body: body
        })).then(function (resp) {
          return resp.json().catch(function () { return {}; }).then(function (data) {
            if (!resp.ok) throw new Error(data.error || "ошибка сохранения");
            var d = new Date();
            var hh = String(d.getHours()).padStart(2, "0");
            var mm = String(d.getMinutes()).padStart(2, "0");
            setStatus("Сохранено " + hh + ":" + mm, "ok");
          });
        }).catch(function (err) {
          setStatus("Не сохранено: " + ((err && err.message) || "ошибка"), "error");
        });
      }

      function keyOf(el) {
        return el.getAttribute("data-p") + "|" + el.getAttribute("data-account") +
          "|" + el.getAttribute("data-field");
      }

      // Отметка оплаты: клик по точке.
      table.addEventListener("click", function (e) {
        var btn = e.target.closest ? e.target.closest("button.st") : null;
        if (!btn || !table.contains(btn)) return;
        var td = btn.closest(".money-cell");
        var input = q("input.num", td);
        if (!input || amount(input.value) === 0) return;
        btn.classList.toggle("paid");
        paint(td);
        recalc();
        var field = input.getAttribute("data-field") === "advance" ? "advance_paid" : "paid";
        save(input, field, btn.classList.contains("paid") ? "1" : "");
      });

      table.addEventListener("input", function (e) {
        var input = e.target;
        if (!input.classList || !input.classList.contains("num")) return;
        var td = input.closest(".money-cell");
        paint(td);
        recalc();
        var key = keyOf(input);
        clearTimeout(timers[key]);
        timers[key] = setTimeout(function () {
          delete timers[key];
          save(input, input.getAttribute("data-field"), input.value.trim());
        }, 600);
      });

      table.addEventListener("change", function (e) {
        var input = e.target;
        if (!input.classList || !input.classList.contains("num")) return;
        var key = keyOf(input);
        clearTimeout(timers[key]);
        delete timers[key];
        paint(input.closest(".money-cell"));
        recalc();
        save(input, input.getAttribute("data-field"), input.value.trim());
      });
    });
  })();

  // Уведомление в шапке исчезает само.
  // Сообщение о будущем месяце показываем как обычное уведомление.
  (function () {
    var note = q(".note");
    var stack = q("[data-page-flash]");
    if (!note || !stack) return;
    var box = document.createElement("div");
    box.className = "page-flash is-note";
    box.setAttribute("role", "status");
    box.textContent = note.textContent.trim();
    stack.appendChild(box);
    note.remove();
  })();

  document.querySelectorAll("[data-page-flash] .page-flash").forEach(function (el) {
    setTimeout(function () {
      el.classList.add("is-hide");
      setTimeout(function () { el.remove(); }, 300);
    }, 4200);
  });
})();

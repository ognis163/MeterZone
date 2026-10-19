(function () {
  "use strict";

  var qa = function (sel, root) {
    return Array.prototype.slice.call((root || document).querySelectorAll(sel));
  };
  var q = function (sel, root) {
    return (root || document).querySelector(sel);
  };

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

  function recalcMoney() {
    qa("[data-money-row]").forEach(function (row) {
      var acc = row.getAttribute("data-account");
      var c = q("[data-charged='" + acc + "']", row);
      var a = q("[data-advance='" + acc + "']", row);
      var flag = q("[data-paid-flag='" + acc + "']", row);
      var btn = q("[data-paid-toggle='" + acc + "']", row);
      var paidNow = (btn && btn.classList.contains("is-paid")) || (flag && flag.value === "1");
      var charged = c ? parseVal(c.value) || 0 : 0;
      var advance = a ? parseVal(a.value) || 0 : 0;
      if (flag) flag.value = paidNow ? "1" : "";
      if (btn) {
        btn.classList.toggle("is-paid", paidNow);
        btn.classList.toggle("is-unpaid", !paidNow);
        btn.textContent = paidNow ? "Оплачено" : "Оплатить";
        btn.setAttribute("aria-pressed", paidNow ? "true" : "false");
      }
    });
  }

  // ---------- автосохранение ----------
  var entryForm = q("form[data-entry]");
  var statusBox = q("[data-autosave-status] .savebar-status");
  var pending = {}; // key -> функция отправки, выполняется по таймеру
  var saveSeq = 0;

  function setStatus(text, kind) {
    if (!statusBox) return;
    statusBox.textContent = text;
    statusBox.setAttribute("data-state", kind || "");
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
    return fetch(url, {
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
    });
  }

  function flushPending() {
    Object.keys(pending).forEach(function (key) {
      clearTimeout(pending[key]);
      var fn = pending[key];
      delete pending[key];
      if (typeof fn === "function") fn();
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
    var financeStatus = q("[data-finance-status]");

    function setFinanceStatus(text, kind) {
      if (!financeStatus) return;
      financeStatus.textContent = text;
      financeStatus.setAttribute("data-state", kind || "");
    }

    function financeSaved() {
      var d = new Date();
      var hh = ("0" + d.getHours()).slice(-2);
      var mm = ("0" + d.getMinutes()).slice(-2);
      setFinanceStatus("Сохранено " + hh + ":" + mm, "ok");
    }

    function sendMoney(acc, field, value) {
      setFinanceStatus("Сохранение…", "saving");
      return fetch(moneyURL, {
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
          financeSaved();
          return data;
        });
      }).catch(function (err) {
        setFinanceStatus("Не сохранено: " + err.message, "error");
        throw err;
      });
    }

    qa("[data-money-row]").forEach(function (row) {
      var acc = row.getAttribute("data-account");
      qa("input[data-charged], input[data-advance]", row).forEach(function (input) {
        var field = input.hasAttribute("data-charged") ? "charged" : "advance";
        input.addEventListener("change", function () {
          var raw = input.value.trim();
          if (raw !== "" && parseVal(raw) == null) {
            setFinanceStatus("Введите число", "error");
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

  // Плитки объектов: активный объект показываем в начале ленты.
  qa(".objectrail-scroll").forEach(function (rail) {
    var active = q(".object-tile.is-active", rail);
    if (active) rail.scrollLeft = Math.max(0, active.offsetLeft - 6);
  });

  // Полоса месяцев: выбранный месяц ставим к левому краю, остальные — прокруткой.
  qa("[data-month-strip]").forEach(function (strip) {
    var selected = q(".month-cell.is-selected", strip);
    if (selected) {
      var oldBehavior = strip.style.scrollBehavior;
      strip.style.scrollBehavior = "auto";
      strip.scrollLeft = Math.max(0, selected.offsetLeft - 2);
      strip.style.scrollBehavior = oldBehavior;
    }
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
  if (new URLSearchParams(window.location.search).get("print") === "1" && q(".sheet")) {
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

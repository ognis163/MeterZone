package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"indextro/internal/model"
)

func TestSaveMoney(t *testing.T) {
	_, st, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	form := url.Values{
		"charged_water_cold":  {"1 250,50"},
		"paid_water_cold":     {"on"},
		"charged_energy":      {"2 400"},
		"advance_energy":      {"1 000"},
		"advance_paid_energy": {"on"},
	}
	rr := request(t, h, http.MethodPost, "/o/1/readings?m="+now.String(), form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("код %d: %s", rr.Code, rr.Body.String()[:min(400, rr.Body.Len())])
	}
	money, err := st.MoneyForMonth(1, now.String())
	if err != nil {
		t.Fatal(err)
	}
	if money[model.AccountWaterCold].Charged != 1250.5 || money[model.AccountWaterCold].Debt() != 0 {
		t.Fatalf("вода: %+v", money[model.AccountWaterCold])
	}
	if money[model.AccountEnergy].Charged != 2400 || money[model.AccountEnergy].Debt() != 1400 {
		t.Fatalf("электро: %+v", money[model.AccountEnergy])
	}

	// Отметка «оплачен»: долг закрывается, аванс сохраняется.
	form = url.Values{
		"charged_energy": {"2 400"},
		"advance_energy": {"1 000"},
		"paid_energy":    {"on"},
	}
	rr = request(t, h, http.MethodPost, "/o/1/readings?m="+now.String(), form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("оплата: код %d", rr.Code)
	}
	money, _ = st.MoneyForMonth(1, now.String())
	if money[model.AccountEnergy].Debt() != 0 || money[model.AccountEnergy].Overpay() != 1000 {
		t.Fatalf("оплаченный счёт: %+v", money[model.AccountEnergy])
	}

	// Отрицательная сумма отклоняется.
	bad := url.Values{"charged_water_cold": {"-10"}}
	rr = request(t, h, http.MethodPost, "/o/1/readings?m="+now.String(), bad, "http://example.com")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("отрицательная сумма: код %d", rr.Code)
	}
}
func TestSMTPConfigSave(t *testing.T) {
	srv, _, h := newTestServer(t)
	form := url.Values{
		"host":      {"smtp.example.ru"},
		"port":      {"465"},
		"user":      {"uchet@example.ru"},
		"password":  {"secret"},
		"from":      {"uchet@example.ru"},
		"from_name": {"Учёт"},
		"tls":       {"ssl"},
	}
	rr := request(t, h, http.MethodPost, "/settings/smtp", form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("код %d: %s", rr.Code, rr.Body.String()[:min(500, rr.Body.Len())])
	}
	cfg := srv.loadSMTP()
	if cfg.Host != "smtp.example.ru" || cfg.Password != "secret" {
		t.Fatalf("настройки: %+v", cfg)
	}
	page := request(t, h, http.MethodGet, "/settings", nil, "")
	if strings.Contains(page.Body.String(), "secret") {
		t.Fatal("пароль попал в HTML")
	}
}

func TestSendWithoutSMTP(t *testing.T) {
	_, _, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	form := url.Values{
		"object": {"1"},
		"kind":   {"month"},
		"scope":  {"all"},
		"m":      {now.String()},
		"email":  {"test@example.ru"},
	}
	rr := request(t, h, http.MethodPost, "/reports/send", form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("код %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "err=send") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestSubMeterBadge(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodGet, "/?object=1", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "дубль") {
		t.Fatal("нет отметки дублирующего счётчика")
	}
	if !strings.Contains(body, "finance-panel") {
		t.Fatal("нет блока начислений")
	}
	if !strings.Contains(body, "data-save-status") {
		t.Fatal("нет индикатора автосохранения в шапке")
	}
	// Статус один: в карточках его больше нет.
	if strings.Contains(body, "data-autosave-status") || strings.Contains(body, "data-finance-status") {
		t.Fatal("статус автосохранения задвоен")
	}
}

func TestAutoSaveReading(t *testing.T) {
	_, st, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	meters, _ := st.Meters(1, false)
	m := meters[0]
	series, _ := st.ReadingsForObject(1)
	next := series[m.ID][now.String()] + 2

	form := url.Values{"meter": {itoa(m.ID)}, "value": {formatTest(next)}}
	rr := request(t, h, http.MethodPost, "/o/1/save?m="+now.String(), form, "http://example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("не JSON: %q", rr.Header().Get("Content-Type"))
	}
	got, _ := st.ReadingsForMonth(1, now.String())
	if got[m.ID].Value != next {
		t.Fatalf("значение %v, ожидалось %v", got[m.ID].Value, next)
	}

	bad := url.Values{"meter": {itoa(m.ID)}, "value": {"0,1"}}
	rr = request(t, h, http.MethodPost, "/o/1/save?m="+now.String(), bad, "http://example.com")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("низкое значение: код %d", rr.Code)
	}
}

func TestAutoSaveMoneyPatch(t *testing.T) {
	_, st, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	base := "/o/1/money?m=" + now.String()

	if err := st.SaveMoney([]model.Money{
		{ObjectID: 1, Period: now.String(), Account: model.AccountEnergy},
		{ObjectID: 1, Period: now.String(), Account: model.AccountWater},
	}, now.String()); err != nil {
		t.Fatal(err)
	}

	rr := request(t, h, http.MethodPost, base,
		url.Values{"account": {"energy"}, "field": {"charged"}, "value": {"2400"}}, "http://example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("счёт: код %d: %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, base,
		url.Values{"account": {"energy"}, "field": {"advance"}, "value": {"1000"}}, "http://example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("аванс: код %d: %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, base,
		url.Values{"account": {"energy"}, "field": {"advance_paid"}, "value": {"1"}}, "http://example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("оплата аванса: код %d: %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, base,
		url.Values{"account": {"water_cold"}, "field": {"charged"}, "value": {"1200"}}, "http://example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("вода, сумма: код %d: %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, base,
		url.Values{"account": {"water_cold"}, "field": {"paid"}, "value": {"1"}}, "http://example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("оплата воды: код %d: %s", rr.Code, rr.Body.String())
	}
	money, err := st.MoneyForMonth(1, now.String())
	if err != nil {
		t.Fatal(err)
	}
	energy := money[model.AccountEnergy]
	if energy.Charged != 2400 || energy.AdvanceSum != 1000 || energy.PaidFlag {
		t.Fatalf("электро: %+v", energy)
	}
	if energy.Debt() != 1400 {
		t.Fatalf("долг по счёту: %+v", energy)
	}
	if !money[model.AccountWaterCold].PaidFlag {
		t.Fatalf("вода не оплачена: %+v", money[model.AccountWaterCold])
	}
}

func TestSelectedObjectPersistsAcrossPages(t *testing.T) {
	_, _, h := newTestServer(t)

	rr := request(t, h, http.MethodGet, "/?object=2", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("главная: код %d", rr.Code)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == objectCookie && c.Value == "2" {
			found = true
		}
	}
	if !found {
		t.Fatal("выбранный объект не сохранён в cookie")
	}

	withCookie := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "127.0.0.1:34567"
		req.AddCookie(&http.Cookie{Name: objectCookie, Value: "2"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// /reports — старая ссылка: она перенаправляет в журнал с тем же объектом.
	redirect := withCookie("/reports?object=2")
	if redirect.Code != http.StatusFound {
		t.Fatalf("отчёты: код %d", redirect.Code)
	}
	if loc := redirect.Header().Get("Location"); !strings.Contains(loc, "/journal") {
		t.Fatalf("старая ссылка ведёт не в журнал: %q", loc)
	}

	journal := withCookie("/journal")
	if journal.Code != http.StatusOK {
		t.Fatalf("журнал: код %d", journal.Code)
	}
	journalBody := journal.Body.String()
	if !strings.Contains(journalBody, "object=2") || !strings.Contains(journalBody, "is-active") {
		t.Fatal("в журнале выбран не тот объект")
	}
}
func TestThemeAsset(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodGet, "/assets/theme.js", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "localStorage") {
		t.Fatal("в theme.js нет сохранения темы")
	}
}

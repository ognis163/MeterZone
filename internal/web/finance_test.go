package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"indextro/internal/model"
)

func TestFinanceHidesMoneyPanel(t *testing.T) {
	_, st, h := newTestServer(t)
	obj := model.Object{Name: "Без финансов", Kind: model.KindHouse}
	if err := st.SaveObject(&obj); err != nil {
		t.Fatal(err)
	}
	m := model.Meter{ObjectID: obj.ID, Resource: model.WaterCold, Label: "Кухня"}
	if err := st.SaveMeter(&m); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMoney([]model.Money{{ObjectID: obj.ID, Period: "2026-10", Account: model.AccountWaterCold, Charged: 100, Paid: 50}}, "2026-10"); err != nil {
		t.Fatal(err)
	}

	rr := request(t, h, http.MethodGet, "/?object="+itoa(obj.ID)+"&m=2026-10", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "finance-panel") {
		t.Fatal("при выключенном финучёте блок денег виден")
	}

	m.FinanceEnabled = true
	if err := st.SaveMeter(&m); err != nil {
		t.Fatal(err)
	}
	rr = request(t, h, http.MethodGet, "/?object="+itoa(obj.ID)+"&m=2026-10", nil, "")
	if !strings.Contains(rr.Body.String(), "finance-panel") {
		t.Fatal("при включённом финучёте блока денег нет")
	}
}

func TestPortSetting(t *testing.T) {
	srv, _, h := newTestServer(t)
	form := url.Values{"port": {"19099"}}
	rr := request(t, h, http.MethodPost, "/settings/port", form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("код %d: %s", rr.Code, rr.Body.String()[:min(300, rr.Body.Len())])
	}
	if srv.Port != 19099 {
		t.Fatalf("порт в памяти %d", srv.Port)
	}
	if srv.Store.Setting("http_port", "") != "19099" {
		t.Fatal("порт не сохранён")
	}

	bad := url.Values{"port": {"70000"}}
	rr = request(t, h, http.MethodPost, "/settings/port", bad, "http://example.com")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("некорректный порт: код %d", rr.Code)
	}
}

func TestReportsPreviewUsesResourceNames(t *testing.T) {
	_, _, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	rr := request(t, h, http.MethodGet, "/reports/preview?object=1&kind=range&from="+now.String()+"&to="+now.String()+"&scope=all", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"Ведомость показаний", "ХВС", "ГВС", "кВт·ч", "Приборы учёта"} {
		if !strings.Contains(body, want) {
			t.Fatalf("в ведомости нет %q", want)
		}
	}
	if strings.Contains(body, ">Кухня<") {
		t.Fatal("в предпросмотре остались названия точек установки")
	}
}

func TestParseMoneyRejectsNaN(t *testing.T) {
	for _, raw := range []string{"NaN", "nan", "+Inf", "-Inf", "Infinity"} {
		if _, err := parseMoney(raw); err == nil {
			t.Fatalf("%q принят как сумма", raw)
		}
	}
}

// TestRecordTotals проверяет арифметику строки таблицы начислений:
// оплачено — по отметкам позиций, к оплате — всё остальное.
func TestRecordTotals(t *testing.T) {
	rec := model.Record{AdvanceSum: 12000, InvoiceSum: 8500, InvoicePaid: true}
	if got := rec.Total(); got != 20500 {
		t.Fatalf("итого: %v, ожидалось 20500", got)
	}
	if got := rec.Paid(); got != 8500 {
		t.Fatalf("оплачено: %v, ожидалось 8500", got)
	}
	if got := rec.Due(); got != 12000 {
		t.Fatalf("к оплате: %v, ожидалось 12000", got)
	}
	if got := (model.Record{AdvanceSum: 1000, AdvancePaid: true}).Due(); got != 0 {
		t.Fatalf("оплаченный аванс: к оплате %v", got)
	}
}

// TestJournalMoneyRespectsScope — блок «Начисления» в журнале суммирует
// только те счета, которые выбраны фильтром «Что включить».
func TestJournalMoneyRespectsScope(t *testing.T) {
	_, st, h := newTestServer(t)
	period := "2026-10"
	items := []model.Money{
		{ObjectID: 1, Period: period, Account: model.AccountWaterCold, Charged: 1000, PaidFlag: true},
		{ObjectID: 1, Period: period, Account: model.AccountWaterHot, Charged: 500, PaidFlag: true},
		{ObjectID: 1, Period: period, Account: model.AccountEnergy, Charged: 3000, PaidFlag: true},
	}
	for _, it := range items {
		if err := st.SaveMoney([]model.Money{it}, period); err != nil {
			t.Fatal(err)
		}
	}

	// scope → ожидаемый итог карточки месяца и число ячеек счетов в ней.
	cases := map[string]struct {
		total string
		cells int
	}{
		"all":    {"4 500 ₽", 6}, // ХВС, ГВС, электро — аванс и счёт
		"water":  {"1 500 ₽", 4}, // ХВС и ГВС
		"energy": {"3 000 ₽", 2}, // только электро
	}
	for scope, want := range cases {
		rr := request(t, h, http.MethodGet,
			"/journal?object=1&from=2026-10&to=2026-10&scope="+scope, nil, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: код %d", scope, rr.Code)
		}
		body := rr.Body.String()
		if !strings.Contains(body, `data-card-total>`+want.total) {
			t.Fatalf("%s: нет итога %q в карточке месяца", scope, want.total)
		}
		card := body[strings.Index(body, `data-row="2026-10"`):]
		card = card[:strings.Index(card, "</article>")]
		if got := strings.Count(card, `class="money-cell`); got != want.cells {
			t.Fatalf("%s: ячеек счетов %d, ожидалось %d", scope, got, want.cells)
		}
	}
}

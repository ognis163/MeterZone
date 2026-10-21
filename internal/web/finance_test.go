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

// TestJournalMoneyRespectsScope — таблица «Начисления» в журнале
// показывает только те счета, которые выбраны фильтром «Что включить».
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

	// scope → строки ресурсов и суммы за месяц.
	type want struct {
		labels []string
		absent []string
		totals []string
	}
	cases := map[string]want{
		"all": {
			labels: []string{"Холодная вода", "Горячая вода", "Электроэнергия"},
			totals: []string{"3 000 ₽", "1 000 ₽", "500 ₽"},
		},
		"water": {
			labels: []string{"Холодная вода", "Горячая вода"},
			absent: []string{"Электроэнергия"},
			totals: []string{"1 000 ₽", "500 ₽"},
		},
		"energy": {
			labels: []string{"Электроэнергия"},
			absent: []string{"Холодная вода", "Горячая вода"},
			totals: []string{"3 000 ₽"},
		},
	}
	for scope, w := range cases {
		rr := request(t, h, http.MethodGet,
			"/journal?object=1&from=2026-10&to=2026-10&scope="+scope, nil, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: код %d", scope, rr.Code)
		}
		body := rr.Body.String()
		if !strings.Contains(body, `class="money-table"`) {
			t.Fatalf("%s: нет таблицы начислений", scope)
		}
		for _, label := range w.labels {
			if !strings.Contains(body, ">"+label+"</span>") {
				t.Fatalf("%s: нет строки %q", scope, label)
			}
		}
		for _, label := range w.absent {
			if strings.Contains(body, ">"+label+"</span>") {
				t.Fatalf("%s: лишняя строка %q", scope, label)
			}
		}
		for _, sum := range w.totals {
			if !strings.Contains(body, `data-total>`+sum) {
				t.Fatalf("%s: нет итога %q", scope, sum)
			}
		}
	}
}

// TestJournalMoneyMarksPositionsSeparately — аванс и счёт отмечаются
// оплаченными по отдельности, а «Итого» показывает остаток.
func TestJournalMoneyMarksPositionsSeparately(t *testing.T) {
	_, st, h := newTestServer(t)
	period := "2026-10"
	if err := st.SaveMoney([]model.Money{{
		ObjectID: 1, Period: period, Account: model.AccountEnergy,
		Charged: 3000, AdvanceSum: 1000, AdvancePaid: true,
	}}, period); err != nil {
		t.Fatal(err)
	}
	rr := request(t, h, http.MethodGet,
		"/journal?object=1&from=2026-10&to=2026-10&scope=energy", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()
	if got := strings.Count(body, "money-cell is-paid"); got != 1 {
		t.Fatalf("оплаченных позиций %d, ожидалась 1", got)
	}
	if got := strings.Count(body, "money-cell is-unpaid"); got != 1 {
		t.Fatalf("неоплаченных позиций %d, ожидалась 1", got)
	}
	if !strings.Contains(body, "к оплате 3 000 ₽") {
		t.Fatal("нет остатка к оплате по счёту")
	}
}

// TestJournalMoneyHiddenWithoutFinance — таблица начислений не показывается,
// если ни у одного прибора не включён финансовый учёт.
func TestJournalMoneyHiddenWithoutFinance(t *testing.T) {
	_, st, h := newTestServer(t)
	meters, err := st.Meters(1, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := range meters {
		m := meters[i]
		m.FinanceEnabled = false
		m.FinanceMode = model.FinanceNone
		if err := st.SaveMeter(&m); err != nil {
			t.Fatal(err)
		}
	}
	rr := request(t, h, http.MethodGet, "/journal?object=1&from=2026-10&to=2026-10", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, `class="money-table"`) {
		t.Fatal("таблица начислений показана без финансового учёта")
	}
	if !strings.Contains(body, "Финансовый учёт не включён") {
		t.Fatal("нет подсказки про финансовый учёт")
	}
}

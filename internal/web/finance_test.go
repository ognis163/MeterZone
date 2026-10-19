package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

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
	if strings.Contains(rr.Body.String(), "Финансовый учёт") {
		t.Fatal("при выключенном финучёте блок денег виден")
	}

	m.FinanceEnabled = true
	if err := st.SaveMeter(&m); err != nil {
		t.Fatal(err)
	}
	rr = request(t, h, http.MethodGet, "/?object="+itoa(obj.ID)+"&m=2026-10", nil, "")
	if !strings.Contains(rr.Body.String(), "Финансовый учёт") {
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
	rr := request(t, h, http.MethodGet, "/reports?object=1", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Холодное водоснабжение") {
		t.Fatal("в предпросмотре нет раздела ХВС")
	}
	if !strings.Contains(body, "Горячее водоснабжение") {
		t.Fatal("в предпросмотре нет раздела ГВС")
	}
	if strings.Contains(body, ">Кухня<") {
		t.Fatal("в предпросмотре остались названия точек установки")
	}
}

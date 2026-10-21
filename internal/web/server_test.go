package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"indextro/internal/model"
	"indextro/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"), time.Local)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SeedDemo(); err != nil {
		t.Fatalf("demo: %v", err)
	}
	font, err := os.ReadFile("../../DejaVuSans.ttf")
	if err != nil {
		t.Fatalf("font: %v", err)
	}
	srv, err := New(st, time.Local, "test", font, filepath.Join(dir, "backups"), 8099)
	if err != nil {
		t.Fatalf("web: %v", err)
	}
	return srv, st, srv.Handler()
}

func request(t *testing.T, h http.Handler, method, target string, form url.Values, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = "127.0.0.1:34567"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestPagesRender(t *testing.T) {
	_, _, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	cases := []struct{ path, want string }{
		{"/", "Показания"},
		{"/?object=1", "Холодное водоснабжение"},
		{"/journal", "Журнал"},
		{"/journal", "Журнал"},
		{"/objects", "Объекты"},
		{"/objects/1/edit", "Приборы учёта"},
		{"/meters/new?object=1", "Новый прибор"},
		{"/settings", "Резервные копии"},
		{"/reports/preview?object=1&kind=month&m=" + now.String() + "&scope=all", "Сведения"},
		{"/reports/preview?object=1&kind=range&from=2026-01&to=2026-12&scope=water", "Ведомость"},
		{"/reports/preview?object=1&kind=month&m=2024-01&scope=all", "не вносились"},
		{"/journal?object=1&year=2024", "показаний нет"},
	}
	for _, c := range cases {
		rr := request(t, h, http.MethodGet, c.path, nil, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s: код %d", c.path, rr.Code)
			continue
		}
		body := rr.Body.String()
		if !strings.Contains(body, c.want) {
			t.Errorf("%s: нет %q", c.path, c.want)
		}
		if !strings.Contains(body, "</html>") {
			t.Errorf("%s: шаблон не дорисован", c.path)
		}
	}
}

func TestNotFoundAndSecurityHeaders(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodGet, "/nope", nil, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rr.Code)
	}
	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Error("нет CSP")
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("нет nosniff")
	}
}

func TestLocalNetworkGuard(t *testing.T) {
	_, _, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("внешний адрес пущен: %d", rr.Code)
	}
}

func TestSaveReadings(t *testing.T) {
	_, st, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	meters, err := st.Meters(1, false)
	if err != nil || len(meters) == 0 {
		t.Fatalf("приборы: %v", err)
	}
	m := meters[0]
	series, _ := st.ReadingsForObject(1)
	old := series[m.ID][now.String()]
	next := old + 1.5

	form := url.Values{"v_" + itoa(m.ID): {formatTest(next)}}
	rr := request(t, h, http.MethodPost, "/o/1/readings?m="+now.String(), form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("код %d, тело: %s", rr.Code, rr.Body.String()[:min(400, rr.Body.Len())])
	}
	got, _ := st.ReadingsForMonth(1, now.String())
	if got[m.ID].Value != next {
		t.Fatalf("значение %v, ожидалось %v", got[m.ID].Value, next)
	}
}

func TestSaveReadingsRejectsLowerValue(t *testing.T) {
	_, st, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	meters, _ := st.Meters(1, false)
	m := meters[0]
	form := url.Values{"v_" + itoa(m.ID): {"0,1"}}
	rr := request(t, h, http.MethodPost, "/o/1/readings?m="+now.String(), form, "http://example.com")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "меньше предыдущего") {
		t.Fatalf("нет сообщения об ошибке: %s", rr.Body.String()[:min(500, rr.Body.Len())])
	}
}

func TestCrossSitePostRejected(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodPost, "/settings/backup", url.Values{}, "http://evil.example")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("код %d", rr.Code)
	}
	// Sec-Fetch-Site: cross-site без Origin.
	req := httptest.NewRequest(http.MethodPost, "/settings/backup", strings.NewReader(""))
	req.RemoteAddr = "127.0.0.1:34567"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("cross-site без Origin: %d", rr2.Code)
	}
}

func TestPDFEndpoint(t *testing.T) {
	_, _, h := newTestServer(t)
	now := model.PeriodOf(time.Now())
	rr := request(t, h, http.MethodGet,
		"/pdf?object=1&kind=month&m="+now.String()+"&scope=water", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.HasPrefix(rr.Body.String(), "%PDF") {
		t.Fatal("тело не PDF")
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
}

func TestCreateObjectAndBackup(t *testing.T) {
	_, st, h := newTestServer(t)
	before, _ := st.Objects(false)

	form := url.Values{
		"name":    {"Новый объект"},
		"kind":    {"commercial"},
		"address": {"ул. Примерная, 5"},
		"owner":   {"ООО Тест"},
	}
	rr := request(t, h, http.MethodPost, "/objects/save", form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("создание объекта: код %d, %s", rr.Code, rr.Body.String()[:min(400, rr.Body.Len())])
	}
	after, _ := st.Objects(false)
	if len(after) != len(before)+1 {
		t.Fatalf("объектов %d, было %d", len(after), len(before))
	}

	rr = request(t, h, http.MethodPost, "/settings/backup", url.Values{}, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("бэкап: код %d", rr.Code)
	}
	list, _ := st.ListBackups(filepath.Join(filepath.Dir(st.Path()), "backups"))
	if len(list) == 0 {
		t.Fatal("резервная копия не создана")
	}
}

func TestCSVExport(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodGet, "/export.csv", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("Content-Type = %q", rr.Header().Get("Content-Type"))
	}
	body := rr.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatal("нет BOM для Excel")
	}
	if !strings.Contains(string(body), "Показания") {
		t.Fatal("нет заголовков CSV")
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func formatTest(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

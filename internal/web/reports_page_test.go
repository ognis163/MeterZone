package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"indextro/internal/model"
)

// TestPreviewUsesDocumentLayout проверяет, что предпросмотр рисуется
// из общей модели документа, а не отдельной вёрсткой.
func TestPreviewUsesDocumentLayout(t *testing.T) {
	_, _, h := newTestServer(t)
	now := model.PeriodOf(time.Now())

	rr := request(t, h, http.MethodGet,
		"/reports/preview?object=1&kind=month&m="+now.String()+"&scope=all", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("сведения: код %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`class="doc-sheet doc-month"`,
		"Сведения о показаниях",
		`class="doc-table"`,
		"Показания сняты",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("в сведениях нет %q", want)
		}
	}
	if strings.Contains(body, `class="sheet"`) {
		t.Fatal("осталась старая обёртка листа")
	}

	rr = request(t, h, http.MethodGet,
		"/reports/preview?object=1&kind=range&from="+now.String()+"&to="+now.String()+"&scope=all", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("ведомость: код %d", rr.Code)
	}
	body = rr.Body.String()
	for _, want := range []string{
		`class="doc-sheet doc-range"`,
		"Ведомость показаний",
		"Приборы учёта",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("в ведомости нет %q", want)
		}
	}
}

// TestReportsRedirectsToJournal — страницы «Отчёты» больше нет: старые
// ссылки ведут в журнал с тем же объектом, чтобы ничего не сломалось.
func TestReportsRedirectsToJournal(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodGet, "/reports?object=1", nil, "")
	if rr.Code != http.StatusFound {
		t.Fatalf("код %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/journal") || !strings.Contains(loc, "object=1") {
		t.Fatalf("редирект %q", loc)
	}
}
func TestBackupEmailSettings(t *testing.T) {
	srv, _, h := newTestServer(t)
	form := url.Values{
		"backup_email_enabled": {"1"},
		"backup_email":         {"backup@example.ru"},
	}
	rr := request(t, h, http.MethodPost, "/settings/backup-email", form, "http://example.com")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("код %d: %s", rr.Code, rr.Body.String()[:min(400, rr.Body.Len())])
	}
	if srv.Store.Setting("backup_email_enabled", "") != "1" {
		t.Fatal("галочка не сохранена")
	}
	if srv.Store.Setting("backup_email", "") != "backup@example.ru" {
		t.Fatal("адрес не сохранён")
	}

	bad := url.Values{"backup_email_enabled": {"1"}, "backup_email": {"плохой"}}
	rr = request(t, h, http.MethodPost, "/settings/backup-email", bad, "http://example.com")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("некорректный адрес: код %d", rr.Code)
	}
}

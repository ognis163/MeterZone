package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestReportsObjectSelectionDoesNotOpenPreview(t *testing.T) {
	_, _, h := newTestServer(t)
	rr := request(t, h, http.MethodGet, "/reports?object=1", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `action="/reports"`) {
		t.Fatal("форма должна перезагружать страницу отчётов")
	}
	if strings.Contains(body, `action="/reports/preview"`) {
		t.Fatal("выбор объекта не должен вести на предпросмотр")
	}
	if !strings.Contains(body, `class="month-strip`) {
		t.Fatal("нет полосы месяцев")
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

// Package web — HTTP-интерфейс приложения.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"indextro/internal/model"
	"indextro/internal/report"
	"indextro/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed assets/*
var assetsFS embed.FS

var pageNames = []string{
	"index", "home_empty", "entry", "journal", "reports", "preview",
	"objects", "object_form", "meter_form", "settings", "error",
}

// Server — веб-приложение.
type Server struct {
	Store        *store.Store
	Loc          *time.Location
	Version      string
	Font         []byte
	AssetVersion string
	BackupDir    string
	Port         int
	OnPortChange func(int) error
	Started      time.Time

	pages  map[string]*template.Template
	assets http.Handler
}

// New создаёт сервер и разбирает шаблоны.
func New(st *store.Store, loc *time.Location, version string, font []byte, backupDir string, port int) (*Server, error) {
	if loc == nil {
		loc = time.Local
	}
	s := &Server{
		Store:     st,
		Loc:       loc,
		Version:   version,
		Font:      font,
		BackupDir: backupDir,
		Port:      port,
		Started:   time.Now(),
	}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New("layout").Funcs(templateFuncs()).ParseFS(templatesFS,
			"templates/layout.html",
			"templates/partials.html",
			"templates/meter_fields.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("шаблон %s: %w", name, err)
		}
		pages[name] = t
	}
	s.pages = pages

	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, err
	}
	s.assets = http.StripPrefix("/assets/", http.FileServerFS(sub))
	s.AssetVersion = assetsVersion()
	return s, nil
}

// Base — общие поля страницы.
type Base struct {
	Title        string
	Version      string
	AssetVersion string
	Nav          string
	Flash        string
	Error        string
	Now          time.Time

	Objects            []model.Object
	ObjectID           int64
	ObjectName         string
	ObjectSwitchAction string
	ObjectSwitchQuery  template.URL
}

// Handler собирает маршруты и middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /o/{id}", s.handleEntry)
	mux.HandleFunc("POST /o/{id}/readings", s.handleSaveReadings)
	mux.HandleFunc("POST /o/{id}/save", s.handleAutoSave)
	mux.HandleFunc("POST /o/{id}/money", s.handlePatchMoney)
	mux.HandleFunc("POST /o/{id}/clear", s.handleClearMonth)

	mux.HandleFunc("GET /journal", s.handleJournal)

	mux.HandleFunc("GET /reports", s.handleReportsForm)
	mux.HandleFunc("GET /reports/preview", s.handlePreview)
	mux.HandleFunc("GET /pdf", s.handlePDF)
	mux.HandleFunc("POST /reports/send", s.handleSendReport)

	mux.HandleFunc("GET /objects", s.handleObjects)
	mux.HandleFunc("GET /objects/new", s.handleObjectNew)
	mux.HandleFunc("GET /objects/{id}/edit", s.handleObjectEdit)
	mux.HandleFunc("POST /objects/save", s.handleObjectSave)
	mux.HandleFunc("POST /objects/{id}/archive", s.handleObjectArchive)
	mux.HandleFunc("POST /objects/{id}/delete", s.handleObjectDelete)

	mux.HandleFunc("GET /meters/new", s.handleMeterNew)
	mux.HandleFunc("GET /meters/{id}/edit", s.handleMeterEdit)
	mux.HandleFunc("POST /meters/save", s.handleMeterSave)
	mux.HandleFunc("POST /meters/{id}/archive", s.handleMeterArchive)
	mux.HandleFunc("POST /meters/{id}/delete", s.handleMeterDelete)

	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("POST /settings/backup", s.handleBackupNow)
	mux.HandleFunc("POST /settings/backup-email", s.handleBackupEmailSave)
	mux.HandleFunc("POST /settings/port", s.handlePortSave)
	mux.HandleFunc("GET /settings/backup/{name}", s.handleBackupDownload)
	mux.HandleFunc("POST /settings/import", s.handleImport)
	mux.HandleFunc("POST /settings/smtp", s.handleSMTPSave)
	mux.HandleFunc("POST /settings/smtp/test", s.handleSMTPTest)
	mux.HandleFunc("POST /settings/restore/{name}", s.handleRestoreBackup)
	mux.HandleFunc("GET /export.csv", s.handleExportCSV)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.Handle("GET /assets/", cacheMW(s.assets))

	return s.recoverMW(s.securityMW(s.localMW(s.originMW(mux))))
}

func (s *Server) base(title, nav string) Base {
	return Base{Title: title, Version: s.Version, AssetVersion: s.AssetVersion, Nav: nav, Now: time.Now().In(s.Loc)}
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("шаблон %s: %v", page, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, status int, title, msg string) {
	b := s.base(title, "")
	b.Error = msg
	s.render(w, status, "error", struct {
		Base
		Message string
	}{b, msg})
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// flashFor переводит код в сообщение (без отражения произвольного текста).
func flashFor(r *http.Request) string {
	switch r.URL.Query().Get("ok") {
	case "saved":
		return "Показания сохранены."
	case "cleared":
		return "Показания за месяц удалены."
	case "object":
		return "Объект сохранён."
	case "meter":
		return "Прибор сохранён."
	case "archived":
		return "Запись перемещена в архив."
	case "deleted":
		return "Запись удалена."
	case "backup":
		return "Резервная копия создана."
	case "backupmail":
		return "Настройка копий на почту сохранена."
	case "port":
		return "Порт изменён."
	case "imported":
		return "Данные импортированы."
	case "restored":
		return "Данные восстановлены из резервной копии."
	case "sent":
		return "Письмо с PDF отправлено."
	case "smtp":
		return "Настройки почты сохранены."
	case "smtptest":
		return "Тестовое письмо отправлено."
	}
	return ""
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"fmtVal":     model.FormatValue,
		"fmtInput":   model.FormatInput,
		"money":      model.FormatMoney,
		"dateRu":     model.FormatDateRu,
		"dateTimeRu": model.FormatDateTimeRu,
		"period":     func(p model.Period) string { return p.Label() },
		"short":      func(p model.Period) string { return p.ShortLabel() },
		"monthShort": func(p model.Period) string { return p.MonthShort() },
		"genitive":   func(p model.Period) string { return p.Genitive() },
		"accusative": func(p model.Period) string { return p.Accusative() },
		"add":        func(a, b int) int { return a + b },
		"sub":        func(a, b int) int { return a - b },
		"plural":     model.PluralRu,
		"pct": func(a, b int) int {
			if b <= 0 {
				return 0
			}
			return a * 100 / b
		},
		"size": formatBytes,
		"dash": func(s string) string {
			if strings.TrimSpace(s) == "" {
				return "—"
			}
			return s
		},
		"statusClass": func(st report.Status) string {
			switch {
			case st.Total == 0:
				return "empty"
			case st.Filled == 0:
				return "empty"
			case st.Filled < st.Total:
				return "partial"
			default:
				return "ok"
			}
		},
		"statusText": func(st report.Status) string {
			switch {
			case st.Total == 0:
				return "Нет приборов"
			case st.Filled == 0:
				return "Показания не внесены"
			case st.Filled < st.Total:
				return fmt.Sprintf("Внесено %d из %d", st.Filled, st.Total)
			default:
				return "Все показания внесены"
			}
		},
		"hasResource": func(list []model.Resource, r model.Resource) bool {
			for _, v := range list {
				if v == r {
					return true
				}
			}
			return false
		},
		"asset":     func(name string) string { return path.Join("/assets", name) },
		"kindLabel": model.KindLabel,
	}
}

func assetsVersion() string {
	h := sha256.New()
	_ = fs.WalkDir(assetsFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := assetsFS.ReadFile(p)
		if err != nil {
			return nil
		}
		h.Write([]byte(p))
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f МБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f КБ", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d Б", n)
}

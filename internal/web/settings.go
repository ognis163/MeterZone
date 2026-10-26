package web

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"indextro/internal/mail"
	"indextro/internal/model"
	"indextro/internal/store"
)

type settingsPageData struct {
	Base
	Stats              store.Stats
	Backups            []store.Backup
	BackupsSize        int64
	BackupDir          string
	DBPath             string
	Port               int
	PortLocked         bool
	Uptime             string
	Imported           *store.ImportStats
	Timezone           string
	SMTP               smtpView
	BackupEmailEnabled bool
	BackupEmail        string
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	stats := s.Store.Stats()
	backups, _ := s.Store.ListBackups(s.BackupDir)
	var size int64
	for _, b := range backups {
		size += b.Size
	}
	base := s.base("Настройки", "settings")
	base.Flash = s.flash(r)
	s.withObjectContext(w, r, &base, 0, "/")
	data := settingsPageData{
		Base:               base,
		Stats:              stats,
		Backups:            backups,
		BackupsSize:        size,
		BackupDir:          s.BackupDir,
		DBPath:             s.Store.Path(),
		Port:               s.Port,
		PortLocked:         portLocked(),
		Uptime:             humanUptime(time.Since(s.Started)),
		Timezone:           timezoneLabel(s.Loc),
		SMTP:               smtpToView(s.loadSMTP()),
		BackupEmailEnabled: s.Store.Setting("backup_email_enabled", "") == "1",
		BackupEmail:        s.Store.Setting("backup_email", ""),
	}
	if r.URL.Query().Get("ok") == "imported" {
		o, _ := strconv.Atoi(r.URL.Query().Get("o"))
		m, _ := strconv.Atoi(r.URL.Query().Get("m"))
		rr, _ := strconv.Atoi(r.URL.Query().Get("rd"))
		data.Imported = &store.ImportStats{Objects: o, Meters: m, Readings: rr}
	}
	s.render(w, http.StatusOK, "settings", data)
}

func humanUptime(d time.Duration) string {
	if d < time.Minute {
		return "меньше минуты"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", days, model.PluralRu(days, "день", "дня", "дней")))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d ч", hours))
	}
	if mins > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf("%d мин", mins))
	}
	return strings.Join(parts, " ")
}

func (s *Server) handleBackupNow(w http.ResponseWriter, r *http.Request) {
	b, err := s.Store.Backup(s.BackupDir)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось создать резервную копию.")
		return
	}
	_, _ = s.Store.PruneBackups(s.BackupDir, 14)
	s.NotifyBackup(b.Path)
	s.redirect(w, r, "/settings?ok=backup")
}

func (s *Server) handlePortSave(w http.ResponseWriter, r *http.Request) {
	if portLocked() {
		s.settingsError(w, r, "Порт задаётся при запуске контейнера. Измените его в docker-compose.yml и перезапустите.")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.settingsError(w, r, "Не удалось прочитать форму.")
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	if err != nil || port < 1 || port > 65535 {
		s.settingsError(w, r, "Порт должен быть числом от 1 до 65535.")
		return
	}
	old := s.Port
	if port != old && s.OnPortChange != nil {
		if err := s.OnPortChange(port); err != nil {
			s.settingsError(w, r, "Не удалось открыть порт "+strconv.Itoa(port)+". Возможно, он занят.")
			return
		}
	}
	if err := s.Store.SetSetting("http_port", strconv.Itoa(port)); err != nil {
		s.settingsError(w, r, "Не удалось сохранить порт.")
		return
	}
	s.Port = port
	if port == old || s.OnPortChange == nil {
		s.redirect(w, r, "/settings?ok=port")
		return
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	http.Redirect(w, r, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+"/settings?ok=port", http.StatusSeeOther)
}

func (s *Server) handleBackupEmailSave(w http.ResponseWriter, r *http.Request) {
	enabled := r.FormValue("backup_email_enabled") == "1"
	email := strings.TrimSpace(r.FormValue("backup_email"))
	if enabled && !validEmail(email) {
		s.settingsError(w, r, "Укажите корректный адрес для резервных копий.")
		return
	}
	flag := ""
	if enabled {
		flag = "1"
	}
	if err := s.Store.SetSetting("backup_email_enabled", flag); err != nil {
		s.settingsError(w, r, "Не удалось сохранить настройку.")
		return
	}
	if err := s.Store.SetSetting("backup_email", email); err != nil {
		s.settingsError(w, r, "Не удалось сохранить адрес.")
		return
	}
	s.redirect(w, r, "/settings?ok=backupmail#backup")
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".db") {
		s.fail(w, http.StatusNotFound, "Файл не найден", "Некорректное имя файла.")
		return
	}
	full := filepath.Join(s.BackupDir, name)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		s.fail(w, http.StatusNotFound, "Файл не найден", "Копия не найдена.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeFile(w, r, full)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".db") {
		s.fail(w, http.StatusNotFound, "Файл не найден", "Некорректное имя копии.")
		return
	}
	full := filepath.Join(s.BackupDir, name)
	if _, err := os.Stat(full); err != nil {
		s.fail(w, http.StatusNotFound, "Файл не найден", "Копия не найдена.")
		return
	}
	stats, err := s.Store.RestoreFrom(full, s.BackupDir)
	if err != nil {
		s.settingsError(w, r, "Восстановление не выполнено: "+err.Error())
		return
	}
	s.redirect(w, r, fmt.Sprintf("/settings?ok=restored&o=%d&m=%d&rd=%d", stats.Objects, stats.Meters, stats.Readings))
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		s.settingsError(w, r, "Файл слишком большой или форма повреждена.")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.settingsError(w, r, "Выберите файл базы .db.")
		return
	}
	defer file.Close()
	if header.Size > 64<<20 {
		s.settingsError(w, r, "Файл больше 64 МБ.")
		return
	}
	tmp, err := os.CreateTemp("", "meterzone-import-*.db")
	if err != nil {
		s.settingsError(w, r, "Не удалось сохранить файл на сервере.")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.ReadFrom(file); err != nil {
		tmp.Close()
		s.settingsError(w, r, "Не удалось прочитать файл.")
		return
	}
	if err := tmp.Close(); err != nil {
		s.settingsError(w, r, "Не удалось сохранить файл.")
		return
	}
	stats, err := s.Store.ImportLegacy(tmpName)
	if err != nil {
		s.settingsError(w, r, "Импорт не выполнен: "+err.Error())
		return
	}
	s.redirect(w, r, fmt.Sprintf("/settings?ok=imported&o=%d&m=%d&rd=%d", stats.Objects, stats.Meters, stats.Readings))
}

func (s *Server) settingsError(w http.ResponseWriter, r *http.Request, msg string) {
	base := s.base("Настройки", "settings")
	base.Error = msg
	s.withObjectContext(w, r, &base, 0, "/")
	backups, _ := s.Store.ListBackups(s.BackupDir)
	s.render(w, http.StatusBadRequest, "settings", settingsPageData{
		Base:               base,
		Stats:              s.Store.Stats(),
		Backups:            backups,
		BackupDir:          s.BackupDir,
		DBPath:             s.Store.Path(),
		Port:               s.Port,
		PortLocked:         portLocked(),
		Uptime:             humanUptime(time.Since(s.Started)),
		Timezone:           timezoneLabel(s.Loc),
		SMTP:               smtpToView(s.loadSMTP()),
		BackupEmailEnabled: s.Store.Setting("backup_email_enabled", "") == "1",
		BackupEmail:        s.Store.Setting("backup_email", ""),
	})
}

// portLocked сообщает, что порт задан снаружи (Docker): менять его
// из интерфейса нельзя, иначе приложение переедет на порт, который
// наружу не проброшен, и станет недоступным.
func portLocked() bool { return os.Getenv("PORT_FIXED") == "1" }

func (s *Server) handleSMTPSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.settingsError(w, r, "Не удалось прочитать форму.")
		return
	}
	old := s.loadSMTP()
	cfg := smtpSettings{
		Host:     strings.TrimSpace(r.PostFormValue("host")),
		User:     strings.TrimSpace(r.PostFormValue("user")),
		From:     strings.TrimSpace(r.PostFormValue("from")),
		FromName: strings.TrimSpace(r.PostFormValue("from_name")),
		TLS:      r.PostFormValue("tls"),
		Password: old.Password,
	}
	if v := strings.TrimSpace(r.PostFormValue("port")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	} else {
		cfg.Port = 465
	}
	if pw := r.PostFormValue("password"); pw != "" {
		cfg.Password = pw
	}
	if r.PostFormValue("clear_password") == "1" {
		cfg.Password = ""
	}
	if cfg.TLS == "" {
		cfg.TLS = "auto"
	}
	if cfg.From != "" && !validEmail(cfg.From) {
		s.settingsError(w, r, "Некорректный адрес отправителя.")
		return
	}
	if cfg.Host != "" {
		if err := cfg.config().Validate(); err != nil {
			s.settingsError(w, r, "Почта: "+err.Error())
			return
		}
	}
	if err := s.saveSMTP(cfg); err != nil {
		s.settingsError(w, r, "Не удалось сохранить настройки почты.")
		return
	}
	s.redirect(w, r, "/settings?ok=smtp")
}

func (s *Server) handleSMTPTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.settingsError(w, r, "Не удалось прочитать форму.")
		return
	}
	cfg := s.loadSMTP()
	if cfg.From == "" {
		s.settingsError(w, r, "Сначала укажите адрес отправителя и сохраните настройки.")
		return
	}
	to := strings.TrimSpace(r.PostFormValue("test_email"))
	if to == "" {
		to = cfg.From
	}
	if !validEmail(to) {
		s.settingsError(w, r, "Некорректный адрес для теста.")
		return
	}
	if err := mail.Send(cfg.config(), to, "MeterZone: проверка почты",
		"Это тестовое письмо. Если вы его видите, отправка документов настроена.", "", nil); err != nil {
		s.settingsError(w, r, "Тест не пройден: "+err.Error())
		return
	}
	s.redirect(w, r, "/settings?ok=smtptest")
}

// periodRangeText — «Октябрь 2026» или «Январь — Декабрь 2026» для имени файла.
func periodRangeText(from, to string) string {
	pf, err1 := model.ParsePeriod(from)
	pt, err2 := model.ParsePeriod(to)
	if err1 != nil || err2 != nil {
		return from + " — " + to
	}
	if pf == pt {
		return pf.Label()
	}
	return pf.Label() + " — " + pt.Label()
}

// handleExportCSV выгружает показания в CSV. Журнал передаёт объект и
// период — тогда в файл попадают только выбранные месяцы; без параметров
// выгружается всё (ссылка из настроек).
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	var objectID int64
	if v := strings.TrimSpace(r.URL.Query().Get("object")); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			objectID = id
		}
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))

	rows, err := s.Store.ReadingsExportPeriod(objectID, from, to)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось выгрузить данные.")
		return
	}

	name := "Показания — " + time.Now().In(s.Loc).Format("02.01.2006") + ".csv"
	if objectID != 0 {
		if obj, err := s.Store.Object(objectID); err == nil {
			name = "Показания — " + objectPlace(obj)
			if from != "" && to != "" {
				name += " — " + periodRangeText(from, to)
			}
			name += ".csv"
		}
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"meterzone-readings.csv\"; filename*=UTF-8'"+url.PathEscape(sanitizeFileName(name)))
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.Write([]string{"Объект", "Адрес", "Собственник", "Прибор", "Заводской номер",
		"Ресурс", "Период", "Показания", "Расход"})
	last := map[string]float64{}
	for _, e := range rows {
		key := e.Object + "|" + e.Meter + "|" + e.Serial + "|" + e.Resource
		cons := ""
		if prev, ok := last[key]; ok {
			if e.Value >= prev {
				cons = model.FormatValue(model.Round3(e.Value - prev))
			}
		}
		last[key] = e.Value
		cw.Write([]string{
			e.Object, e.Address, e.Owner, e.Meter, e.Serial,
			model.Resource(e.Resource).Label(), e.Period,
			model.FormatValue(e.Value), cons,
		})
	}
	cw.Flush()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"version": s.Version,
		"uptime":  time.Since(s.Started).Round(time.Second).String(),
		"db":      s.Store.Path(),
	})
}

func timezoneLabel(loc *time.Location) string {
	if loc == nil || loc.String() == "Local" {
		return "системный"
	}
	return loc.String()
}

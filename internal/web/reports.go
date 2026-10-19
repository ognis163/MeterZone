package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"indextro/internal/mail"
	"indextro/internal/model"
	"indextro/internal/pdfdoc"
	"indextro/internal/report"
)

type reportRowView struct {
	Label string
	Value string
	Unit  string
	Note  string
}

type reportFormData struct {
	Base
	Objects  []model.Object
	ObjectID int64
	Object   model.Object
	Kind     string
	Month    model.Period
	From     model.Period
	To       model.Period
	Months   []model.Period
	Scope    string
	HasData  bool
	Strip    monthStripView

	HasPreview  bool
	PeriodText  string
	MeterCount  int
	MonthsCount int
	WaterTotal  float64
	EnergyTotal float64
	PreviewRows []reportRowView

	OpenHref  string
	PDFHref   string
	PrintHref string

	EmailWater  string
	EmailEnergy string
	EmailTo     string
	SelfEmail   string
	SMTPReady   bool
}

func (s *Server) handleReportsForm(w http.ResponseWriter, r *http.Request) {
	objects, err := s.Store.Objects(false)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось прочитать объекты.")
		return
	}
	now := time.Now().In(s.Loc)
	current := model.PeriodOf(now)
	base := s.base("Отчёты", "reports")
	base.Flash = s.flash(r)
	base.Error = ""
	shared := s.periodContext(r)
	data := reportFormData{
		Base:    base,
		Objects: objects,
		Kind:    "month",
		Month:   current,
		From:    model.Period{Year: now.Year(), Month: 1},
		To:      model.Period{Year: now.Year(), Month: 12},
		Scope:   scopeValue(r),
	}
	if len(objects) > 0 {
		// Объект из адреса, иначе — выбранный ранее в шапке (cookie), иначе первый.
		data.ObjectID = s.selectedObjectID(r)
	}
	if k := r.FormValue("kind"); k == "range" {
		data.Kind = "range"
	}
	if data.ObjectID == 0 {
		s.render(w, http.StatusOK, "reports", data)
		return
	}

	od, err := s.loadObjectData(data.ObjectID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	data.Object = od.Object
	s.withObjectContext(w, r, &data.Base, data.ObjectID, "/reports")

	months, err := s.Store.AvailableMonths(data.ObjectID)
	if err == nil {
		data.Months = months
	}
	if len(months) > 0 {
		data.HasData = true
		first, last := months[0], months[len(months)-1]
		// Месяц общий для всех страниц: выбранный на главной или в журнале.
		data.Month = shared
		if !containsPeriod(months, shared) {
			data.Month = last
		}
		rememberPeriod(w, data.Month)
		data.From = first
		data.To = last
		if v := r.FormValue("m_mobile"); v != "" {
			if p, err := model.ParsePeriod(v); err == nil && containsPeriod(months, p) {
				data.Month = p
			}
		} else if v := r.FormValue("m"); v != "" {
			if p, err := model.ParsePeriod(v); err == nil && containsPeriod(months, p) {
				data.Month = p
			}
		}
		if v := r.FormValue("from"); v != "" {
			if p, err := model.ParsePeriod(v); err == nil && containsPeriod(months, p) {
				data.From = p
			}
		}
		if v := r.FormValue("to"); v != "" {
			if p, err := model.ParsePeriod(v); err == nil && containsPeriod(months, p) {
				data.To = p
			}
		}
		if data.To.Before(data.From) {
			data.From, data.To = data.To, data.From
		}
	}

	href := func(p model.Period) string {
		q := url.Values{}
		q.Set("object", strconv.FormatInt(data.ObjectID, 10))
		q.Set("kind", "month")
		q.Set("scope", data.Scope)
		q.Set("m", p.String())
		return "/reports?" + q.Encode()
	}
	data.Strip = buildMonthStrip(data.Month, monthsMap(months), href, true, current)
	if data.HasData {
		if p, ok := nearestMonthInYear(months, data.Month.Year-1, data.Month.Month); ok {
			data.Strip.PrevHref = href(p)
		} else {
			data.Strip.PrevHref = ""
		}
		if p, ok := nearestMonthInYear(months, data.Month.Year+1, data.Month.Month); ok {
			data.Strip.NextHref = href(p)
		} else {
			data.Strip.NextHref = ""
		}
	}

	// Мини-предпросмотр выбранного месяца или периода.
	if data.HasData {
		if data.Kind == "month" {
			rep := report.MonthReport(od, data.Month, data.Scope, now)
			data.PeriodText = data.Month.Label()
			data.MeterCount = rep.Total
			data.HasPreview = rep.HasReadings
			for _, sec := range rep.Sections {
				switch sec.Resource {
				case model.WaterCold, model.WaterHot:
					data.WaterTotal = model.Round3(data.WaterTotal + sec.Total)
				case model.Energy:
					data.EnergyTotal = model.Round3(data.EnergyTotal + sec.Total)
				}
				data.PreviewRows = append(data.PreviewRows, reportRowView{
					Label: sec.Title,
					Value: model.FormatValue(sec.Total),
					Unit:  sec.Unit,
				})
			}
			if rep.HasReadings {
				q := url.Values{}
				q.Set("object", strconv.FormatInt(data.ObjectID, 10))
				q.Set("kind", "month")
				q.Set("scope", data.Scope)
				q.Set("m", data.Month.String())
				data.OpenHref = "/reports/preview?" + q.Encode()
				data.PDFHref = "/pdf?" + q.Encode()
				data.PrintHref = data.OpenHref + "&print=1"
			}
		} else {
			rrep := report.RangeReport(od, data.From, data.To, data.Scope, now)
			data.PeriodText = data.From.Label() + " — " + data.To.Label()
			data.MeterCount = len(rrep.MeterTotals)
			data.MonthsCount = data.From.MonthDiff(data.To) + 1
			data.HasPreview = rrep.HasData
			for _, rsc := range rrep.Resources {
				total := rrep.ResourceTotals[rsc]
				data.PreviewRows = append(data.PreviewRows, reportRowView{
					Label: rsc.Section(),
					Value: model.FormatValue(total),
					Unit:  rsc.Unit(),
					Note:  "за период",
				})
				switch rsc {
				case model.WaterCold, model.WaterHot:
					data.WaterTotal = model.Round3(data.WaterTotal + total)
				case model.Energy:
					data.EnergyTotal = model.Round3(data.EnergyTotal + total)
				}
			}
			q := url.Values{}
			q.Set("object", strconv.FormatInt(data.ObjectID, 10))
			q.Set("kind", "range")
			q.Set("scope", data.Scope)
			q.Set("from", data.From.String())
			q.Set("to", data.To.String())
			data.OpenHref = "/reports/preview?" + q.Encode()
			data.PDFHref = "/pdf?" + q.Encode()
			data.PrintHref = data.OpenHref + "&print=1"
		}
	}

	data.EmailWater = od.Object.EmailWater
	data.EmailEnergy = od.Object.EmailEnergy
	data.EmailTo = reportEmail(od.Object, data.Scope)
	cfg := s.loadSMTP()
	data.SelfEmail = cfg.From
	if data.SelfEmail == "" && validEmail(cfg.User) {
		data.SelfEmail = cfg.User
	}
	data.SMTPReady = cfg.config().Validate() == nil

	s.render(w, http.StatusOK, "reports", data)
}

// nearestMonthInYear выбирает месяц указанного года, ближайший к prefer.
func nearestMonthInYear(months []model.Period, year, prefer int) (model.Period, bool) {
	var best model.Period
	bestDiff := 99
	for _, p := range months {
		if p.Year != year {
			continue
		}
		d := p.Month - prefer
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			best, bestDiff = p, d
		}
	}
	return best, bestDiff != 99
}

func containsPeriod(list []model.Period, p model.Period) bool {
	for _, v := range list {
		if v == p {
			return true
		}
	}
	return false
}

type previewPageData struct {
	Base
	Kind       string
	Month      report.Month
	Range      report.Range
	Object     model.Object
	Scope      string
	ScopeText  string
	PDFHref    string
	BackHref   string
	PeriodText string

	ObjectField string
	KindField   string
	ScopeField  string
	MField      string
	FromField   string
	ToField     string

	EmailTo     string
	EmailWater  string
	EmailEnergy string
	SelfEmail   string
	SMTPReady   bool
}

func scopeText(scope string) string {
	switch scope {
	case "water":
		return "Вода"
	case "energy":
		return "Электроэнергия"
	}
	return "Всё"
}

func scopeValue(r *http.Request) string {
	if v := r.FormValue("scope"); v == "water" || v == "energy" {
		return v
	}
	return "all"
}

func periodValue(r *http.Request, key string, def model.Period) model.Period {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return def
	}
	p, err := model.ParsePeriod(raw)
	if err != nil {
		return def
	}
	return p
}

func (s *Server) reportDoc(r *http.Request) (kind string, monthRep report.Month, rangeRep report.Range, obj model.Object, scope string, from, to, month model.Period, err error) {
	now := time.Now().In(s.Loc)
	scope = scopeValue(r)
	month = periodValue(r, "m", model.PeriodOf(now))
	kind = r.FormValue("kind")
	if kind != "month" && kind != "range" {
		kind = "month"
	}
	from = periodValue(r, "from", model.Period{Year: now.Year(), Month: 1})
	to = periodValue(r, "to", model.Period{Year: now.Year(), Month: 12})

	idRaw := r.FormValue("object")
	id, convErr := strconv.ParseInt(idRaw, 10, 64)
	if convErr != nil || id <= 0 {
		var objects []model.Object
		objects, err = s.Store.Objects(false)
		if err != nil || len(objects) == 0 {
			err = fmt.Errorf("нет объектов для отчёта")
			return
		}
		id = objects[0].ID
	}
	var od report.ObjectData
	od, err = s.loadObjectData(id)
	if err != nil {
		err = fmt.Errorf("объект не найден")
		return
	}
	obj = od.Object
	if kind == "month" {
		monthRep = report.MonthReport(od, month, scope, now)
	} else {
		if to.Before(from) {
			err = fmt.Errorf("конец периода раньше начала")
			return
		}
		if from.MonthDiff(to) > 120 {
			err = fmt.Errorf("период больше 10 лет")
			return
		}
		rangeRep = report.RangeReport(od, from, to, scope, now)
	}
	return
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	kind, monthRep, rangeRep, obj, scope, from, to, month, err := s.reportDoc(r)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Не удалось построить отчёт", err.Error())
		return
	}
	base := s.base("Предпросмотр", "reports")
	base.Flash = s.flash(r)
	if r.URL.Query().Get("err") == "send" {
		base.Error = "Не удалось отправить письмо. Проверьте настройки почты и адрес получателя."
	}
	cfg := s.loadSMTP()
	data := previewPageData{
		Base:        base,
		Kind:        kind,
		Month:       monthRep,
		Range:       rangeRep,
		Object:      obj,
		Scope:       scope,
		ScopeText:   scopeText(scope),
		ObjectField: strconv.FormatInt(obj.ID, 10),
		KindField:   kind,
		ScopeField:  scope,
		MField:      month.String(),
		FromField:   from.String(),
		ToField:     to.String(),
		SMTPReady:   cfg.config().Validate() == nil,
	}
	data.EmailTo = reportEmail(obj, scope)
	data.EmailWater = obj.EmailWater
	data.EmailEnergy = obj.EmailEnergy
	cfg = s.loadSMTP()
	data.SelfEmail = cfg.From
	if data.SelfEmail == "" && validEmail(cfg.User) {
		data.SelfEmail = cfg.User
	}
	data.SMTPReady = cfg.config().Validate() == nil
	if kind == "month" {
		data.PeriodText = month.Label()
	} else {
		data.PeriodText = from.Label() + " — " + to.Label()
	}
	q := url.Values{}
	q.Set("object", data.ObjectField)
	q.Set("kind", kind)
	q.Set("scope", scope)
	if kind == "month" {
		q.Set("m", month.String())
	} else {
		q.Set("from", from.String())
		q.Set("to", to.String())
	}
	data.PDFHref = "/pdf?" + q.Encode()
	data.BackHref = "/reports?" + q.Encode()
	s.withObjectContext(w, r, &data.Base, obj.ID, "/reports")
	s.render(w, http.StatusOK, "preview", data)
}

func reportEmail(obj model.Object, scope string) string {
	switch scope {
	case "water":
		return obj.EmailWater
	case "energy":
		return obj.EmailEnergy
	}
	if obj.EmailWater != "" {
		return obj.EmailWater
	}
	return obj.EmailEnergy
}

func (s *Server) handleSendReport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, http.StatusBadRequest, "Ошибка", "Не удалось прочитать форму.")
		return
	}
	kind, monthRep, rangeRep, obj, scope, from, to, month, err := s.reportDoc(r)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Не удалось построить документ", err.Error())
		return
	}
	back := previewHref(obj, kind, scope, month, from, to)
	cfg := s.loadSMTP()
	if err := cfg.config().Validate(); err != nil {
		s.redirect(w, r, back+"&err=send")
		return
	}
	toEmail := strings.TrimSpace(r.FormValue("email"))
	if !validEmail(toEmail) || toEmail == "" {
		s.redirect(w, r, back+"&err=send")
		return
	}
	var data []byte
	if kind == "month" {
		data, err = pdfdoc.MonthPDF(monthRep, s.Font)
	} else {
		data, err = pdfdoc.RangePDF(rangeRep, s.Font)
	}
	if err != nil {
		s.redirect(w, r, back+"&err=send")
		return
	}
	name := reportFileName(kind, scope, obj, month, from, to)
	period := month.Label()
	if kind == "range" {
		period = from.Label() + " — " + to.Label()
	}
	subject := fmt.Sprintf("Показания — %s — %s", obj.Title(), period)
	if kind == "range" {
		subject = fmt.Sprintf("Ведомость — %s — %s", obj.Title(), period)
	}
	body := fmt.Sprintf("Здравствуйте!\n\nНаправляю %s по объекту «%s» за %s.\n\nФайл: %s\n\n—\nMeterZone",
		map[bool]string{true: "ведомость", false: "сведения"}[kind == "range"], obj.Title(), period, name)
	if err := mail.Send(cfg.config(), toEmail, subject, body, name, data); err != nil {
		log.Printf("отправка письма %s: %v", toEmail, err)
		s.redirect(w, r, back+"&err=send")
		return
	}
	s.redirect(w, r, back+"&ok=sent")
}

func previewHref(obj model.Object, kind, scope string, month, from, to model.Period) string {
	q := url.Values{}
	q.Set("object", strconv.FormatInt(obj.ID, 10))
	q.Set("kind", kind)
	q.Set("scope", scope)
	if kind == "month" {
		q.Set("m", month.String())
	} else {
		q.Set("from", from.String())
		q.Set("to", to.String())
	}
	return "/reports/preview?" + q.Encode()
}

func (s *Server) handlePDF(w http.ResponseWriter, r *http.Request) {
	kind, monthRep, rangeRep, obj, scope, from, to, month, err := s.reportDoc(r)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Не удалось построить документ", err.Error())
		return
	}
	var data []byte
	if kind == "month" {
		data, err = pdfdoc.MonthPDF(monthRep, s.Font)
	} else {
		data, err = pdfdoc.RangePDF(rangeRep, s.Font)
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка PDF", "Не удалось сформировать PDF.")
		return
	}
	name := reportFileName(kind, scope, obj, month, from, to)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition",
		`attachment; filename="meterzone-report.pdf"; filename*=UTF-8''`+url.PathEscape(name))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(data); err != nil {
		return
	}
}

func reportFileName(kind, scope string, obj model.Object, month, from, to model.Period) string {
	prefix := "Сведения"
	if kind == "range" {
		prefix = "Ведомость"
	}
	switch scope {
	case "water":
		prefix = prefix + " о воде"
	case "energy":
		prefix = prefix + " об электроэнергии"
	}
	period := month.Label()
	if kind == "range" {
		if from.Year == to.Year {
			period = fmt.Sprintf("%s–%s %d", from.MonthName(), to.MonthName(), to.Year)
		} else {
			period = fmt.Sprintf("%s %d – %s %d", from.MonthName(), from.Year, to.MonthName(), to.Year)
		}
	}
	name := strings.Join([]string{prefix, obj.Title(), period}, " — ")
	return sanitizeFileName(name) + ".pdf"
}

func sanitizeFileName(s string) string {
	repl := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "-",
		"\n", " ", "\r", " ", "\t", " ",
	)
	s = repl.Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 120 {
		s = string(r[:120])
	}
	return strings.TrimSpace(s)
}

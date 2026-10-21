package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"indextro/internal/docgen"
	"indextro/internal/mail"
	"indextro/internal/model"
	"indextro/internal/report"
)

// handleReportsForm перенаправляет старые ссылки на объединённый журнал.
func (s *Server) handleReportsForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out := url.Values{}
	if v := q.Get("object"); v != "" {
		out.Set("object", v)
	}
	if v := q.Get("scope"); v == "water" || v == "energy" {
		out.Set("scope", v)
	}
	if q.Get("kind") == "range" {
		if v := q.Get("from"); v != "" {
			out.Set("from", v)
		}
		if v := q.Get("to"); v != "" {
			out.Set("to", v)
		}
	} else if v := q.Get("m"); v != "" {
		out.Set("from", v)
		out.Set("to", v)
	}
	http.Redirect(w, r, "/journal?"+out.Encode(), http.StatusFound)
}

type previewPageData struct {
	Base
	Doc      docgen.Document
	PDFHref   string
	BackHref  string
	BackLabel string

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

// buildDocument собирает печатный документ из готового отчёта.
func buildDocument(kind string, month report.Month, rng report.Range) docgen.Document {
	if kind == "month" {
		return docgen.Month(month)
	}
	return docgen.Range(rng)
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

// docRequest — собранный документ и параметры, нужные ссылкам и письму.
type docRequest struct {
	doc   docgen.Document
	obj   model.Object
	kind  string
	scope string
	month model.Period
	from  model.Period
	to    model.Period
}

// docObject — объект для документа: из адреса, cookie или первый доступный.
func (s *Server) docObject(r *http.Request) (model.Object, error) {
	if id := s.selectedObjectID(r); id != 0 {
		if o, err := s.Store.Object(id); err == nil {
			return o, nil
		}
	}
	objects, err := s.Store.Objects(false)
	if err != nil || len(objects) == 0 {
		return model.Object{}, fmt.Errorf("нет объектов для отчёта")
	}
	return objects[0], nil
}

// buildDocRequest собирает любой документ по параметрам адреса:
// month — сведения за месяц, range — ведомость показаний,
// charges — ведомость начислений. Все три открываются одной страницей.
func (s *Server) buildDocRequest(r *http.Request) (docRequest, error) {
	kind := strings.TrimSpace(r.FormValue("kind"))
	scope := scopeValue(r)
	now := time.Now().In(s.Loc)

	if kind == "charges" {
		obj, err := s.docObject(r)
		if err != nil {
			return docRequest{}, err
		}
		from := periodValue(r, "from", model.Period{Year: now.Year(), Month: 1})
		to := periodValue(r, "to", model.Period{Year: now.Year(), Month: 12})
		if to.Before(from) {
			from, to = to, from
		}
		if from.MonthDiff(to) > 120 {
			return docRequest{}, fmt.Errorf("период больше 10 лет")
		}
		yd := s.buildMoneyRange(obj, from, to, model.PeriodOf(now), scope, false)
		return docRequest{
			doc:   chargesDocument(yd),
			obj:   obj,
			kind:  "charges",
			scope: scope,
			month: to,
			from:  from,
			to:    to,
		}, nil
	}

	_, monthRep, rangeRep, obj, docScope, from, to, month, err := s.reportDoc(r)
	if err != nil {
		return docRequest{}, err
	}
	if kind != "month" && kind != "range" {
		kind = "month"
	}
	return docRequest{
		doc:   buildDocument(kind, monthRep, rangeRep),
		obj:   obj,
		kind:  kind,
		scope: docScope,
		month: month,
		from:  from,
		to:    to,
	}, nil
}

// query — параметры документа для ссылок на PDF и отправку письма.
func (d docRequest) query() url.Values {
	q := url.Values{}
	q.Set("object", strconv.FormatInt(d.obj.ID, 10))
	q.Set("kind", d.kind)
	q.Set("scope", d.scope)
	if d.kind == "month" {
		q.Set("m", d.month.String())
		q.Set("from", d.month.String())
		q.Set("to", d.month.String())
	} else {
		q.Set("from", d.from.String())
		q.Set("to", d.to.String())
	}
	return q
}

// backHref — «Назад» из документа всегда ведёт в журнал за тот же период:
// документы открывают из журнала и с главной.
func (d docRequest) backHref() string {
	q := url.Values{}
	q.Set("object", strconv.FormatInt(d.obj.ID, 10))
	if d.kind == "month" {
		q.Set("from", d.month.String())
		q.Set("to", d.month.String())
	} else {
		q.Set("from", d.from.String())
		q.Set("to", d.to.String())
	}
	return "/journal?" + q.Encode()
}

// periodLabel — период документа словами.
func (d docRequest) periodLabel() string {
	if d.kind == "month" {
		return d.month.Label()
	}
	if d.from == d.to {
		return d.from.Label()
	}
	return d.from.Label() + " — " + d.to.Label()
}

// fileName — имя файла при скачивании и отправке.
func (d docRequest) fileName() string {
	if d.kind == "charges" {
		name := strings.Join([]string{"Ведомость начислений", objectPlace(d.obj), d.periodLabel()}, " — ")
		return sanitizeFileName(name) + ".pdf"
	}
	return reportFileName(d.kind, d.scope, d.obj, d.month, d.from, d.to)
}

// reportEmail — адрес получателя для выбранного набора ресурсов.
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
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	req, err := s.buildDocRequest(r)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Не удалось построить отчёт", err.Error())
		return
	}
	base := s.base("Предпросмотр", "journal")
	base.Flash = s.flash(r)
	if r.URL.Query().Get("err") == "send" {
		base.Error = "Не удалось отправить письмо. Проверьте настройки почты и адрес получателя."
	}
	cfg := s.loadSMTP()
	q := req.query()
	data := previewPageData{
		Base:        base,
		Doc:         req.doc,
		ObjectField: strconv.FormatInt(req.obj.ID, 10),
		KindField:   req.kind,
		ScopeField:  req.scope,
		MField:      req.month.String(),
		FromField:   req.from.String(),
		ToField:     req.to.String(),
		SMTPReady:   cfg.config().Validate() == nil,
	}
	data.EmailTo = reportEmail(req.obj, req.scope)
	data.EmailWater = req.obj.EmailWater
	data.EmailEnergy = req.obj.EmailEnergy
	data.SelfEmail = cfg.From
	if data.SelfEmail == "" && validEmail(cfg.User) {
		data.SelfEmail = cfg.User
	}
	data.PDFHref = "/pdf?" + q.Encode()
	data.BackHref = req.backHref()
	data.BackLabel = "Журнал"
	s.withObjectContext(w, r, &data.Base, req.obj.ID, "/journal")
	s.render(w, http.StatusOK, "preview", data)
}

func (s *Server) handleSendReport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, http.StatusBadRequest, "Ошибка", "Не удалось прочитать форму.")
		return
	}
	req, err := s.buildDocRequest(r)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Не удалось построить документ", err.Error())
		return
	}
	back := "/reports/preview?" + req.query().Encode()
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
	pdf, err := docgen.PDF(req.doc, s.Font)
	if err != nil {
		s.redirect(w, r, back+"&err=send")
		return
	}
	name := req.fileName()
	period := req.periodLabel()
	subject := fmt.Sprintf("Показания — %s — %s", req.obj.Title(), period)
	noun := "сведения"
	switch req.kind {
	case "range":
		subject = fmt.Sprintf("Ведомость — %s — %s", req.obj.Title(), period)
		noun = "ведомость"
	case "charges":
		subject = fmt.Sprintf("Ведомость начислений — %s — %s", req.obj.Title(), period)
		noun = "ведомость начислений"
	}
	body := fmt.Sprintf("Здравствуйте!\n\nНаправляю %s по объекту «%s» за %s.\n\nФайл: %s\n\n—\nMeterZone",
		noun, req.obj.Title(), period, name)
	if err := mail.Send(cfg.config(), toEmail, subject, body, name, pdf); err != nil {
		log.Printf("отправка письма %s: %v", toEmail, err)
		s.redirect(w, r, back+"&err=send")
		return
	}
	s.redirect(w, r, back+"&ok=sent")
}

// reportPreviewHref — ссылка на предпросмотр готового документа.
func reportPreviewHref(id int64, kind, scope string, month, from, to model.Period) string {
	q := url.Values{}
	q.Set("object", strconv.FormatInt(id, 10))
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
	req, err := s.buildDocRequest(r)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "Не удалось построить документ", err.Error())
		return
	}
	data, err := docgen.PDF(req.doc, s.Font)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка PDF", "Не удалось сформировать PDF.")
		return
	}
	name := req.fileName()
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
	name := strings.Join([]string{prefix, objectPlace(obj), period}, " — ")
	return sanitizeFileName(name) + ".pdf"
}

// objectPlace — как объект называть в имени файла: адрес, а если его нет,
// название из карточки объекта.
func objectPlace(obj model.Object) string {
	if strings.TrimSpace(obj.Address) != "" {
		return obj.Address
	}
	return obj.Title()
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

package web

import (
	"encoding/csv"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"indextro/internal/docgen"
	"indextro/internal/model"
	"indextro/internal/report"
	"indextro/internal/store"
)

// objectReadingsCSV отдаёт все показания объекта файлом для Excel.
func (s *Server) objectReadingsCSV(w http.ResponseWriter, obj model.Object, rows []store.ExportRow) {
	name := "meterzone-" + obj.Title() + "-показания-" + time.Now().In(s.Loc).Format("2006-01-02") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"meterzone-object-readings.csv\"; filename*=UTF-8''"+url.PathEscape(sanitizeFileName(name)))
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.Write([]string{"Объект", "Адрес", "Собственник", "Прибор", "Заводской номер",
		"Ресурс", "Период", "Показания", "Расход"})
	last := map[string]float64{}
	for _, e := range rows {
		key := e.Meter + "|" + e.Serial + "|" + e.Resource
		cons := ""
		if prev, ok := last[key]; ok && e.Value >= prev {
			cons = model.FormatValue(model.Round3(e.Value - prev))
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

// objectReadingsPDF отдаёт показания объекта документом за весь период.
func (s *Server) objectReadingsPDF(w http.ResponseWriter, obj model.Object, rows []store.ExportRow) {
	od, err := s.loadObjectData(obj.ID)
	if err != nil {
		s.objectDeleteError(w, obj.ID, "")
		return
	}
	first, last := "", ""
	for _, e := range rows {
		if first == "" || e.Period < first {
			first = e.Period
		}
		if last == "" || e.Period > last {
			last = e.Period
		}
	}
	from, err1 := model.ParsePeriod(first)
	to, err2 := model.ParsePeriod(last)
	if err1 != nil || err2 != nil {
		s.objectDeleteError(w, obj.ID, "Не удалось определить период показаний.")
		return
	}
	now := time.Now().In(s.Loc)
	rep := report.RangeReport(od, from, to, "all", now)
	data, err := docgen.PDF(docgen.Range(rep), s.Font)
	if err != nil {
		log.Printf("PDF перед удалением: %v", err)
		s.objectDeleteError(w, obj.ID, "Не удалось сформировать PDF.")
		return
	}
	name := "meterzone-" + obj.Title() + "-показания-" + now.Format("2006-01-02") + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		`attachment; filename="meterzone-object-readings.pdf"; filename*=UTF-8''`+url.PathEscape(sanitizeFileName(name)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// objectDeleteError показывает страницу удаления с сообщением.
func (s *Server) objectDeleteError(w http.ResponseWriter, id int64, msg string) {
	base := s.base("Удаление объекта", "objects")
	base.Error = msg
	s.render(w, http.StatusOK, "object_delete", objectDeleteData{Base: base, Object: model.Object{ID: id}})
}

// objectDeleteSummary — текст для страницы удаления.
func objectDeleteSummary(first, last string) string {
	if first == "" || last == "" {
		return "Показаний нет."
	}
	if first == last {
		return periodLabel(first)
	}
	return periodLabel(first) + " — " + periodLabel(last)
}

func periodLabel(s string) string {
	if p, err := model.ParsePeriod(s); err == nil {
		return p.Label()
	}
	return strings.TrimSpace(s)
}

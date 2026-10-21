package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"indextro/internal/model"
	"indextro/internal/report"
)

// writeJSON отвечает JSON-статусом для автосохранения.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// entryContext разбирает id объекта и месяц, общие для автосохранения.
func (s *Server) entryContext(w http.ResponseWriter, r *http.Request) (model.Object, model.Period, bool) {
	id, ok := parseID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "некорректный объект"})
		return model.Object{}, model.Period{}, false
	}
	obj, err := s.Store.Object(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "объект не найден"})
		return model.Object{}, model.Period{}, false
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не удалось прочитать данные"})
		return model.Object{}, model.Period{}, false
	}
	period := s.periodParam(r, "m", model.PeriodOf(time.Now().In(s.Loc)))
	return obj, period, true
}

// handleAutoSave сохраняет одно поле показания сразу после изменения.
// POST /o/{id}/save  form: meter, value (пусто = удалить показание)
func (s *Server) handleAutoSave(w http.ResponseWriter, r *http.Request) {
	obj, period, ok := s.entryContext(w, r)
	if !ok {
		return
	}
	meterID := parseFormInt(r.PostFormValue("meter"))
	if meterID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "прибор не указан"})
		return
	}
	meters, err := s.Store.Meters(obj.ID, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось прочитать приборы"})
		return
	}
	var meter *model.Meter
	for i := range meters {
		if meters[i].ID == meterID {
			meter = &meters[i]
			break
		}
	}
	if meter == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "прибор не найден"})
		return
	}

	raw := strings.TrimSpace(r.PostFormValue("value"))
	if raw == "" {
		if err := s.Store.DeleteReading(meter.ID, period.String()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось удалить показания"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "diff": nil})
		return
	}
	val, err := model.ParseValue(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	readings, _ := s.Store.ReadingsForObject(obj.ID)
	prev, _, hasPrev := report.PrevReadingBase(*meter, readings[meter.ID], period)
	if hasPrev && val < prev && !report.MeterResetAllows(*meter, period) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "меньше предыдущего показания " + model.FormatValue(prev),
		})
		return
	}
	if next, _, hasNext := report.NextReading(readings[meter.ID], period); hasNext && val > next {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "больше следующего показания " + model.FormatValue(next),
		})
		return
	}
	if err := s.Store.SaveReading(meter.ID, period.String(), val, ""); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить"})
		return
	}
	resp := map[string]any{"ok": true}
	if hasPrev {
		resp["diff"] = model.Round3(val - prev)
		if val < prev && report.MeterResetAllows(*meter, period) && meter.StartValue > 0 {
			resp["diff"] = model.Round3(val - meter.StartValue)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePatchMoney сохраняет одно поле финансов, не перезаписывая остальные.
// POST /o/{id}/money  form: account, field (charged|advance|paid|advance_paid|
// advance_sum|invoice_sum), value
//
// advance_sum и invoice_sum — поля таблицы начислений журнала: сумма аванса
// и сумма счёта независимо от того, какие позиции включены у приборов.
func (s *Server) handlePatchMoney(w http.ResponseWriter, r *http.Request) {
	obj, period, ok := s.entryContext(w, r)
	if !ok {
		return
	}
	field := strings.TrimSpace(r.PostFormValue("field"))
	raw := strings.TrimSpace(r.PostFormValue("value"))

	acc := model.Account(strings.TrimSpace(r.PostFormValue("account")))
	if !model.ValidAccount(string(acc)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неизвестный счёт"})
		return
	}

	var apply func(*model.Money)
	switch field {
	case "charged":
		v, err := parseMoney(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		apply = func(m *model.Money) { m.Charged = model.Round2(v) }
	case "advance":
		v, err := parseMoney(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		apply = func(m *model.Money) { m.AdvanceSum = model.Round2(v) }
	case "paid":
		paid := raw == "1" || raw == "true" || raw == "on"
		apply = func(m *model.Money) { m.PaidFlag = paid }
	case "advance_paid":
		paid := raw == "1" || raw == "true" || raw == "on"
		apply = func(m *model.Money) { m.AdvancePaid = paid }
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неизвестное поле"})
		return
	}

	if err := s.Store.PatchMoney(obj.ID, period.String(), acc, apply); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить"})
		return
	}
	all, _ := s.Store.MoneyForMonth(obj.ID, period.String())
	m := all[acc]
	resp := map[string]any{
		"ok":      true,
		"charged": model.FormatValue(m.Charged),
		"advance": model.FormatValue(m.Advance()),
		"paid":    m.IsPaid(),
		"debt":    fmt.Sprintf("%.2f", m.Debt()),
		"status":  moneyStatus(m),
	}
	writeJSON(w, http.StatusOK, resp)
}

// helper-функции для обработчиков
func parseFormInt(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

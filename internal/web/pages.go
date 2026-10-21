package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"indextro/internal/model"
	"indextro/internal/report"
	"indextro/internal/store"
)

// ---------------------------------------------------------------- помощники

func parseID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (s *Server) periodParam(r *http.Request, key string, def model.Period) model.Period {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	p, err := model.ParsePeriod(raw)
	if err != nil {
		return def
	}
	return p
}

func scopeParam(r *http.Request) string {
	switch r.URL.Query().Get("scope") {
	case "water", "energy":
		return r.URL.Query().Get("scope")
	}
	return "all"
}

func (s *Server) loadObjectData(id int64) (report.ObjectData, error) {
	obj, err := s.Store.Object(id)
	if err != nil {
		return report.ObjectData{}, err
	}
	meters, err := s.Store.Meters(id, true)
	if err != nil {
		return report.ObjectData{}, err
	}
	readings, err := s.Store.ReadingsForObject(id)
	if err != nil {
		return report.ObjectData{}, err
	}
	return report.ObjectData{Object: obj, Meters: meters, Readings: readings}, nil
}

func (s *Server) flash(r *http.Request) string { return flashFor(r) }

// ---------------------------------------------------------------- деньги

type moneyRowView struct {
	Account     string
	Label       string
	IsEnergy    bool
	IsCold      bool
	IsHot       bool
	ChargedText string
	AdvanceText string
	PaidText    string
	DebtText    string
	StatusText  string
	ChargedRaw  float64
	AdvanceRaw  float64
	PaidRaw     float64
	Debt        float64
	Paid        bool
	AdvancePaid bool
	ShowAdvance bool
	ShowBill    bool
}

// formatMoneyInput скрывает ноль: в поле ввода он не нужен.
func formatMoneyInput(v float64) string {
	if v == 0 {
		return ""
	}
	return model.FormatValue(model.Round2(v))
}

func moneyStatus(m model.Money) string {
	if m.PaidFlag {
		return "Счёт оплачен"
	}
	if debt := m.Debt(); debt > 0 {
		return "Долг " + model.FormatMoney(debt)
	}
	return "Не оплачено"
}

func moneyRowViewFor(acc model.Account, m model.Money, chargedText, advanceText string) moneyRowView {
	if chargedText == "" {
		chargedText = formatMoneyInput(m.Charged)
	}
	if advanceText == "" {
		advanceText = formatMoneyInput(m.Advance())
	}
	return moneyRowView{
		Account:     string(acc),
		Label:       acc.Label(),
		IsEnergy:    acc == model.AccountEnergy,
		IsCold:      acc == model.AccountWaterCold || acc == model.AccountWater,
		IsHot:       acc == model.AccountWaterHot,
		ChargedText: chargedText,
		AdvanceText: advanceText,
		PaidText:    model.FormatMoney(m.Paid),
		DebtText:    model.FormatMoney(m.Debt()),
		StatusText:  moneyStatus(m),
		ChargedRaw:  m.Charged,
		AdvanceRaw:  m.Advance(),
		PaidRaw:     m.Paid,
		Debt:        m.Debt(),
		Paid:        m.PaidFlag,
		AdvancePaid: m.AdvancePaid,
		ShowAdvance: true,
		ShowBill:    true,
	}
}

func moneyRowsWithInput(charged, advance map[model.Account]string,
	paidFlags, advancePaidFlags map[model.Account]bool,
	storeMap map[model.Account]model.Money) []moneyRowView {
	out := make([]moneyRowView, 0, 3)
	for _, acc := range model.AccountOrder {
		m := storeMap[acc]
		cText, aText := "", ""
		cVal := m.Charged
		if v, ok := charged[acc]; ok {
			cText = v
			if n, err := parseMoney(v); err == nil {
				cVal = model.Round2(n)
			}
		}
		aVal := m.Advance()
		if v, ok := advance[acc]; ok {
			aText = v
			if n, err := parseMoney(v); err == nil {
				aVal = model.Round2(n)
			}
		}
		billPaid, ok := paidFlags[acc]
		if !ok {
			billPaid = m.PaidFlag
		}
		advPaid, ok := advancePaidFlags[acc]
		if !ok {
			advPaid = m.AdvancePaid
		}
		m.Charged = cVal
		m.AdvanceSum = aVal
		m.AdvancePaid = advPaid
		m.PaidFlag = billPaid
		m.Paid = 0
		if advPaid {
			m.Paid += aVal
		}
		if billPaid {
			m.Paid += cVal
		}
		out = append(out, moneyRowViewFor(acc, m, cText, aText))
	}
	return out
}

// ---------------------------------------------------------------- строки начислений

// ---------------------------------------------------------------- главная

type objectCardView struct {
	Object    model.Object
	Meters    int
	Status    report.Status
	Last      string
	EntryHref string
	Debt      float64
	DebtText  string
	HasDebt   bool
}

type indexSummary struct {
	Objects   int
	Filled    int
	Meters    int
	MeterDone int
	Water     float64
	Energy    float64
	Debt      float64
	HasDebt   bool
	HasData   bool
}

type indexPageData struct {
	Base
	Month     model.Period
	PrevMonth model.Period
	NextMonth model.Period
	Cards     []objectCardView
	Empty     bool
	IsCurrent bool
	Summary   indexSummary
	MonthNav  monthNavView
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.fail(w, http.StatusNotFound, "Страница не найдена", "Такой страницы нет.")
		return
	}
	id := s.selectedObjectID(r)
	if id == 0 {
		base := s.base("Показания", "home")
		s.withObjectContext(w, r, &base, 0, "/")
		s.render(w, http.StatusOK, "home_empty", base)
		return
	}
	obj, err := s.Store.Object(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	period := s.periodContext(r)
	rememberPeriod(w, period)
	data := s.buildEntryData(r, obj, period, nil, nil, nil, nil, nil, "")
	s.withObjectContext(w, r, &data.Base, obj.ID, "/")
	data.Flash = s.flash(r)
	s.render(w, http.StatusOK, "entry", data)
}

// ---------------------------------------------------------------- ввод показаний

type entryMeterView struct {
	Meter       model.Meter
	Prev        float64
	PrevText    string
	Curr        float64
	CurrText    string
	Diff        float64
	PrevRaw     string
	HasPrev     bool
	HasCurr     bool
	HasDiff     bool
	Invalid     bool
	IsSub       bool
	HasChildren bool
	Net         bool
	ParentTitle string
	OffsetText  string
	DerivedText string
	HasOffset   bool
	HasDerived  bool
}

// entrySectionView — зона одной плитки: вода (ХВС + ГВС) или электроэнергия.
type entrySectionView struct {
	Resource   model.Resource
	Title      string
	Unit       string
	ReportHref string
	Groups     []entryGroupView
	Meters     []entryMeterView
}

// entryGroupView — счётчики одного вида ресурса внутри плитки.
type entryGroupView struct {
	Title  string
	Single bool // единственный вид ресурса в плитке — подпись не нужна
	Meters []entryMeterView
}

type entryPageData struct {
	Base
	Object      model.Object
	Period      model.Period
	PrevHref    string
	NextHref    string
	Sections    []entrySectionView
	Money       []moneyRowView
	Filled      int
	Total       int
	HasMeters   bool
	IsFuture    bool
	WaterTotal  float64
	EnergyTotal float64
	DebtTotal   float64
	HasDebt     bool
	MonthNav    monthNavView

	EmailWater  string
	EmailEnergy string
	EmailTo     string
	SMTPReady   bool
	HasWater    bool
	HasEnergy   bool

	FinanceAny        bool
	FinanceWaterCold  bool
	FinanceWaterHot   bool
	FinanceEnergy     bool
	ShowAdvanceCold   bool
	ShowBillCold      bool
	ShowAdvanceHot    bool
	ShowBillHot       bool
	ShowAdvanceEnergy bool
	ShowBillEnergy    bool

	// FinanceTable — начисления за выбранный месяц в стиле журнала.
	FinanceTable []financeRowView
	MonthLabel   string
}

func (s *Server) buildEntryData(r *http.Request, obj model.Object, period model.Period,
	submitted map[int64]string, charged, advance map[model.Account]string,
	paidFlags, advancePaidFlags map[model.Account]bool, errMsg string) entryPageData {

	meters, _ := s.Store.Meters(obj.ID, false)
	meters = report.OrderedMeters(meters)
	hasWater, hasEnergy := false, false
	for _, m := range meters {
		if m.Resource.IsWater() {
			hasWater = true
		} else {
			hasEnergy = true
		}
	}
	readings, _ := s.Store.ReadingsForObject(obj.ID)

	views := make(map[int64]*entryMeterView, len(meters))
	children := map[int64][]int64{}
	for _, m := range meters {
		series := readings[m.ID]
		prev, _, hasPrev := report.PrevReadingBase(m, series, period)
		val, hasCurr := series[period.String()]
		currText := ""
		if hasCurr {
			currText = model.FormatInput(val)
		}
		if v, ok := submitted[m.ID]; ok {
			currText = v
		}
		v := &entryMeterView{
			Meter:    m,
			Prev:     prev,
			PrevText: "—",
			PrevRaw:  strconv.FormatFloat(prev, 'f', -1, 64),
			CurrText: currText,
			HasPrev:  hasPrev,
			HasCurr:  hasCurr,
			IsSub:    m.IsSub(),
		}
		if hasPrev {
			v.PrevText = model.FormatValue(prev)
		}
		if strings.TrimSpace(currText) != "" {
			if parsed, err := model.ParseValue(currText); err == nil {
				v.Curr = parsed
				if hasPrev {
					if parsed >= prev {
						v.Diff = model.Round3(parsed - prev)
						v.HasDiff = true
					} else if report.MeterResetAllows(m, period) {
						// Счётчик заменили: отсчёт идёт от начального значения.
						v.Diff = model.Round3(parsed - m.StartValue)
						v.HasDiff = v.Diff >= 0
					} else {
						v.Invalid = true
					}
				}
			}
		}
		views[m.ID] = v
		if m.ParentID != 0 {
			children[m.ParentID] = append(children[m.ParentID], m.ID)
		}
	}
	for parentID, kids := range children {
		pv := views[parentID]
		if pv == nil {
			continue
		}
		pv.HasChildren = true
		for _, kid := range kids {
			kv := views[kid]
			if kv == nil {
				continue
			}
			kv.ParentTitle = pv.Meter.Title()
			offset, ok := 0.0, false
			if pv.HasPrev && kv.HasPrev {
				offset, ok = model.Round3(pv.Prev-kv.Prev), true
			} else if pv.Meter.StartValue > 0 && kv.Meter.StartValue > 0 {
				offset, ok = model.Round3(pv.Meter.StartValue-kv.Meter.StartValue), true
			}
			if ok {
				kv.HasOffset = true
				kv.OffsetText = model.FormatValue(offset)
				if kv.HasCurr {
					kv.HasDerived = true
					kv.DerivedText = model.FormatValue(model.Round3(kv.Curr + offset))
				}
			}
		}
	}

	// Плитки: одна для водоснабжения (ХВС и ГВС) и одна для электроэнергии.
	sections := make([]entrySectionView, 0, 2)
	sectionIdx := map[bool]int{}
	groupIdx := map[model.Resource]int{}
	filled, total := 0, 0
	var water, energy float64
	for _, m := range meters {
		v := views[m.ID]
		isWater := m.Resource.IsWater()
		idx, ok := sectionIdx[isWater]
		if !ok {
			idx = len(sections)
			sectionIdx[isWater] = idx
			sec := entrySectionView{Resource: m.Resource, Unit: m.Resource.Unit()}
			if isWater {
				sec.Title = "Водоснабжение"
				sec.ReportHref = reportPreviewHref(obj.ID, "month", "water", period, period, period)
			} else {
				sec.Title = m.Resource.Section()
				sec.ReportHref = reportPreviewHref(obj.ID, "month", "energy", period, period, period)
			}
			sections = append(sections, sec)
		}
		gi, ok := groupIdx[m.Resource]
		if !ok || sections[idx].Groups[gi].Title != m.Resource.Section() {
			gi = len(sections[idx].Groups)
			groupIdx[m.Resource] = gi
			sections[idx].Groups = append(sections[idx].Groups, entryGroupView{Title: m.Resource.Section()})
		}
		sections[idx].Groups[gi].Meters = append(sections[idx].Groups[gi].Meters, *v)
		sections[idx].Meters = append(sections[idx].Meters, *v)
		if len(sections[idx].Groups) == 1 {
			sections[idx].Groups[0].Single = !isWater
		}
		total++
		if v.HasCurr {
			filled++
		}
		if !m.IsSub() && v.HasDiff {
			if m.Resource.IsWater() {
				water = model.Round3(water + v.Diff)
			} else {
				energy = model.Round3(energy + v.Diff)
			}
		}
	}

	var money []moneyRowView
	var debt float64
	financeCold, financeHot, financeEnergy := false, false, false
	financeColdAdvance, financeColdBill := false, false
	financeHotAdvance, financeHotBill := false, false
	financeEnergyAdvance, financeEnergyBill := false, false
	for _, m := range meters {
		mode := m.FinanceModeValue()
		if mode == model.FinanceNone {
			continue
		}
		switch m.Resource {
		case model.WaterCold:
			financeCold = true
			financeColdAdvance = financeColdAdvance || m.ShowAdvance()
			financeColdBill = financeColdBill || m.ShowBill()
		case model.WaterHot:
			financeHot = true
			financeHotAdvance = financeHotAdvance || m.ShowAdvance()
			financeHotBill = financeHotBill || m.ShowBill()
		case model.Energy:
			financeEnergy = true
			financeEnergyAdvance = financeEnergyAdvance || m.ShowAdvance()
			financeEnergyBill = financeEnergyBill || m.ShowBill()
		}
	}
	financeAny := financeCold || financeHot || financeEnergy
	if financeAny {
		moneyMap, _ := s.Store.MoneyForMonth(obj.ID, period.String())
		for _, row := range moneyRowsWithInput(charged, advance, paidFlags, advancePaidFlags, moneyMap) {
			acc := model.Account(row.Account)
			if (acc == model.AccountWaterCold || acc == model.AccountWater) && financeCold ||
				acc == model.AccountWaterHot && financeHot ||
				acc == model.AccountEnergy && financeEnergy {
				switch acc {
				case model.AccountWaterCold, model.AccountWater:
					row.ShowAdvance, row.ShowBill = financeColdAdvance, financeColdBill
				case model.AccountWaterHot:
					row.ShowAdvance, row.ShowBill = financeHotAdvance, financeHotBill
				case model.AccountEnergy:
					row.ShowAdvance, row.ShowBill = financeEnergyAdvance, financeEnergyBill
				}
				money = append(money, row)
				debt += row.Debt
			}
		}
	}

	monthRows := make([]financeRowView, 0, len(money))
	for _, row := range money {
		monthRows = append(monthRows, financeRowFrom(row))
	}
	avail, _ := s.Store.AvailableMonths(obj.ID)
	nav := buildMonthNav(period, monthsMap(avail), func(p model.Period) string {
		return fmt.Sprintf("/?object=%d&m=%s", obj.ID, p.String())
	}, model.PeriodOf(time.Now().In(s.Loc)))

	base := s.base(obj.Title(), "home")
	base.Error = errMsg
	// Статус автосохранения живёт в шапке, рядом с названием приложения.
	base.SaveStatus = true
	return entryPageData{
		Base:              base,
		Object:            obj,
		Period:            period,
		PrevHref:          fmt.Sprintf("/?object=%d&m=%s", obj.ID, period.Prev().String()),
		NextHref:          fmt.Sprintf("/?object=%d&m=%s", obj.ID, period.Next().String()),
		Sections:          sections,
		Money:             money,
		Filled:            filled,
		Total:             total,
		HasMeters:         total > 0,
		IsFuture:          period.After(model.PeriodOf(time.Now().In(s.Loc))),
		WaterTotal:        water,
		EnergyTotal:       energy,
		FinanceAny:        financeAny,
		FinanceWaterCold:  financeCold,
		FinanceWaterHot:   financeHot,
		FinanceEnergy:     financeEnergy,
		ShowAdvanceCold:   financeColdAdvance,
		ShowBillCold:      financeColdBill,
		ShowAdvanceHot:    financeHotAdvance,
		ShowBillHot:       financeHotBill,
		ShowAdvanceEnergy: financeEnergyAdvance,
		ShowBillEnergy:    financeEnergyBill,
		FinanceTable:      monthRows,
		MonthLabel:        period.Accusative() + " г.",
		DebtTotal:         model.Round2(debt),
		HasDebt:           debt != 0,
		MonthNav:          nav,
		EmailWater:        obj.EmailWater,
		EmailEnergy:       obj.EmailEnergy,
		EmailTo:           reportEmail(obj, "water"),
		SMTPReady:         s.loadSMTP().config().Validate() == nil,
		HasWater:          hasWater,
		HasEnergy:         hasEnergy,
	}
}

func (s *Server) handleEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный адрес объекта.")
		return
	}
	if _, err := s.Store.Object(id); err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	target := fmt.Sprintf("/?object=%d", id)
	if m := r.URL.Query().Get("m"); m != "" {
		target += "&m=" + m
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleSaveReadings(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный адрес объекта.")
		return
	}
	obj, err := s.Store.Object(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Возможно, он был удалён.")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, http.StatusBadRequest, "Ошибка", "Не удалось прочитать форму.")
		return
	}
	period := s.periodParam(r, "m", model.PeriodOf(time.Now().In(s.Loc)))

	meters, err := s.Store.Meters(id, false)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось прочитать приборы.")
		return
	}
	readings, _ := s.Store.ReadingsForObject(id)
	submitted := map[int64]string{}
	var inputs []store.ReadingInput
	var problems []string

	for _, m := range meters {
		raw := strings.TrimSpace(r.PostFormValue(fmt.Sprintf("v_%d", m.ID)))
		if raw == "" {
			continue
		}
		submitted[m.ID] = raw
		val, err := model.ParseValue(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", m.Title(), err))
			continue
		}
		prev, _, hasPrev := report.PrevReadingBase(m, readings[m.ID], period)
		reset := report.MeterResetAllows(m, period)
		if hasPrev && val < prev && !reset {
			problems = append(problems, fmt.Sprintf("%s: значение %s меньше предыдущего %s",
				m.Title(), model.FormatValue(val), model.FormatValue(prev)))
			continue
		}
		if next, _, hasNext := report.NextReading(readings[m.ID], period); hasNext && val > next && !reset {
			problems = append(problems, fmt.Sprintf("%s: значение %s больше следующего %s",
				m.Title(), model.FormatValue(val), model.FormatValue(next)))
			continue
		}
		inputs = append(inputs, store.ReadingInput{MeterID: m.ID, Value: val})
	}

	financeCold, financeHot, financeEnergy := false, false, false
	financeColdAdvance, financeColdBill := false, false
	financeHotAdvance, financeHotBill := false, false
	financeEnergyAdvance, financeEnergyBill := false, false
	for _, m := range meters {
		mode := m.FinanceModeValue()
		if mode == model.FinanceNone {
			continue
		}
		switch m.Resource {
		case model.WaterCold:
			financeCold = true
			financeColdAdvance = financeColdAdvance || m.ShowAdvance()
			financeColdBill = financeColdBill || m.ShowBill()
		case model.WaterHot:
			financeHot = true
			financeHotAdvance = financeHotAdvance || m.ShowAdvance()
			financeHotBill = financeHotBill || m.ShowBill()
		case model.Energy:
			financeEnergy = true
			financeEnergyAdvance = financeEnergyAdvance || m.ShowAdvance()
			financeEnergyBill = financeEnergyBill || m.ShowBill()
		}
	}
	financeAny := financeCold || financeHot || financeEnergy

	charged := map[model.Account]string{}
	advance := map[model.Account]string{}
	paidFlags := map[model.Account]bool{}
	advancePaidFlags := map[model.Account]bool{}
	var moneyItems []model.Money
	for _, acc := range model.AccountOrder {
		enabled := acc == model.AccountEnergy && financeEnergy ||
			(acc == model.AccountWaterCold || acc == model.AccountWater) && financeCold ||
			acc == model.AccountWaterHot && financeHot
		if !enabled {
			continue
		}
		cRaw := strings.TrimSpace(r.PostFormValue("charged_" + string(acc)))
		aRaw := strings.TrimSpace(r.PostFormValue("advance_" + string(acc)))
		charged[acc] = cRaw
		advance[acc] = aRaw
		paidFlags[acc] = r.PostFormValue("paid_"+string(acc)) != ""
		advancePaidFlags[acc] = r.PostFormValue("advance_paid_"+string(acc)) != ""

		c, err := parseMoney(cRaw)
		if err != nil {
			problems = append(problems, acc.Short()+": счёт — "+err.Error())
			continue
		}
		a, err := parseMoney(aRaw)
		if err != nil {
			problems = append(problems, acc.Short()+": аванс — "+err.Error())
			continue
		}
		paid := 0.0
		if advancePaidFlags[acc] {
			paid += a
		}
		if paidFlags[acc] {
			paid += c
		}
		moneyItems = append(moneyItems, model.Money{
			ObjectID: id, Period: period.String(), Account: acc,
			Charged: model.Round2(c), Paid: model.Round2(paid),
			AdvanceSum: model.Round2(a), AdvancePaid: advancePaidFlags[acc],
			PaidFlag: paidFlags[acc],
		})
	}

	if len(problems) > 0 {
		data := s.buildEntryData(r, obj, period, submitted, charged, advance, paidFlags, advancePaidFlags, strings.Join(problems, ". "))
		s.withObjectContext(w, r, &data.Base, obj.ID, "/")
		s.render(w, http.StatusBadRequest, "entry", data)
		return
	}
	if err := s.Store.SaveReadings(inputs, period.String()); err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось сохранить показания.")
		return
	}
	if financeAny {
		if err := s.Store.SaveMoney(moneyItems, period.String()); err != nil {
			s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось сохранить начисления.")
			return
		}
	}
	s.redirect(w, r, fmt.Sprintf("/?object=%d&m=%s&ok=saved", id, period.String()))
}

func (s *Server) handleClearMonth(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		s.fail(w, http.StatusNotFound, "Объект не найден", "Некорректный адрес объекта.")
		return
	}
	r.ParseForm()
	period := s.periodParam(r, "m", model.PeriodOf(time.Now().In(s.Loc)))
	if _, err := s.Store.DeleteMonth(id, period.String()); err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось удалить показания.")
		return
	}
	s.redirect(w, r, fmt.Sprintf("/?object=%d&m=%s&ok=cleared", id, period.String()))
}

// ---------------------------------------------------------------- журнал

type journalCellView struct {
	Resource    model.Resource
	Short       string
	Unit        string
	Reading     string
	Consumption string
	Has         bool
}

// journalRangeView — быстрый выбор периода в журнале.
type journalRangeView struct {
	Label  string
	Href   string
	Active bool
	From   model.Period
	To     model.Period
	Wide   bool // на телефоне не показываем: не помещается в строку
}

// journalTotalView — итог по ресурсу за выбранный период.
type journalTotalView struct {
	Label       string
	Consumption string
	Unit        string
}

type journalRowView struct {
	Period    model.Period
	Label     string
	Cells     []journalCellView
	HasAny    bool
	Href      string
	IsCurrent bool
}


type journalPageData struct {
	Base
	Object        model.Object
	Objects       []model.Object
	Year          int
	PrevYear      int
	NextYear      int
	Rows          []journalRowView
	Totals        []journalCellView
	HasData       bool
	HasYear       bool
	Resources     []model.Resource
	JournalTable   journalTableData
	MoneyRange     moneyYearData
	MoneyAny       bool
	MoneyFromLabel string
	MoneyToLabel   string
	MonthNav      monthNavView
	SelectedMonth string
	From          model.Period
	To            model.Period
	FromValue     string
	ToValue       string
	FromLabel     string
	ToLabel       string
	RangeLabel    string
	Ranges        []journalRangeView
	BookTotals    []journalTotalView
	HasMonths     bool
	MonthsCount   int

	Scope           string
	ScopeAllHref    string
	ScopeWaterHref  string
	ScopeEnergyHref string

	StatementHref string

	// Выгрузка выбранного объекта и периода из журнала.
	CSVHref    string
	DocPDFHref string
}

func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	objects, err := s.Store.Objects(false)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось прочитать объекты.")
		return
	}
	base := s.base("Журнал и отчёты", "journal")
	base.Flash = s.flash(r)
	data := journalPageData{Base: base, Objects: objects}
	if len(objects) == 0 {
		s.render(w, http.StatusOK, "journal", data)
		return
	}

	obj := objects[0]
	if id := s.selectedObjectID(r); id != 0 {
		if o, err := s.Store.Object(id); err == nil {
			obj = o
		}
	}
	shared := s.periodContext(r)
	today := model.PeriodOf(time.Now().In(s.Loc))
	// По умолчанию текущий год, но можно выбрать любой диапазон.
	from := model.Period{Year: today.Year, Month: 1}
	to := model.Period{Year: today.Year, Month: 12}
	if v := r.URL.Query().Get("from"); v != "" {
		if p, err := model.ParsePeriod(v); err == nil {
			from = p
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if p, err := model.ParsePeriod(v); err == nil {
			to = p
		}
	}
	if v := r.URL.Query().Get("year"); v != "" && r.URL.Query().Get("from") == "" && r.URL.Query().Get("to") == "" {
		if y, err := strconv.Atoi(v); err == nil && y >= 2000 && y <= 2100 {
			from = model.Period{Year: y, Month: 1}
			to = model.Period{Year: y, Month: 12}
		}
	}
	if v := r.URL.Query().Get("range"); v != "" && r.URL.Query().Get("from") == "" && r.URL.Query().Get("to") == "" {
		switch v {
		case "ytd":
			from = model.Period{Year: today.Year, Month: 1}
			to = today
		case "q3":
			from = today.Add(-2)
			to = today
		case "prev":
			from = model.Period{Year: today.Year - 1, Month: 1}
			to = model.Period{Year: today.Year - 1, Month: 12}
		case "2y":
			from = model.Period{Year: today.Year - 1, Month: 1}
			to = model.Period{Year: today.Year, Month: 12}
		}
	}
	if to.Before(from) {
		from, to = to, from
	}
	year := to.Year
	scope := scopeParam(r)

	od, err := s.loadObjectData(obj.ID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось прочитать данные объекта.")
		return
	}
	meters := od.Meters
	active := make([]model.Meter, 0, len(meters))
	for _, m := range meters {
		if !m.Archived {
			active = append(active, m)
			continue
		}
		// Архивный прибор попадает в журнал, если у него есть показания
		// внутри выбранного диапазона.
		for key := range od.Readings[m.ID] {
			if p, err := model.ParsePeriod(key); err == nil && !p.Before(from) && !p.After(to) {
				active = append(active, m)
				break
			}
		}
	}
	meters = report.OrderedMeters(active)

	resources := []model.Resource{}
	for _, rsc := range model.ResourceOrder {
		if !report.Scoped(rsc, scope) {
			continue
		}
		for _, m := range meters {
			if m.Resource == rsc && !m.IsSub() {
				resources = append(resources, rsc)
				break
			}
		}
	}

	rows := make([]journalRowView, 0, 12)
	totals := map[model.Resource]float64{}
	hasYear := false
	monthsCount := 0
	for p := from; !p.After(to); p = p.Next() {
		row := journalRowView{
			Period:    p,
			Label:     p.Label(),
			Href:      fmt.Sprintf("/?object=%d&m=%s", obj.ID, p.String()),
			IsCurrent: p == today,
		}
		rowHas := false
		for _, rsc := range resources {
			cell := journalCellView{Resource: rsc, Short: rsc.Short(), Unit: rsc.Unit()}
			var group []model.Meter
			for _, m := range meters {
				if m.Resource == rsc {
					group = append(group, m)
				}
			}
			var reading float64
			hasReading := false
			var cons float64
			hasCons := false
			for _, m := range group {
				if v, ok := od.Readings[m.ID][p.String()]; ok {
					cell.Has = true
					// Основной прибор показываем как есть; если основных
					// несколько — суммируем. Дубли не суммируем.
					if !m.IsSub() {
						reading = model.Round3(reading + v)
						hasReading = true
					}
				}
				if !!m.IsSub() {
					continue
				}
				if v, ok := od.ConsumptionFor(m, p); ok {
					cons = model.Round3(cons + v)
					hasCons = true
				}
			}
			if hasReading {
				cell.Reading = model.FormatValue(reading)
			}
			if hasCons {
				cell.Consumption = model.FormatValue(cons)
				totals[rsc] = model.Round3(totals[rsc] + cons)
			}
			if cell.Has {
				rowHas = true
				hasYear = true
			}
			row.Cells = append(row.Cells, cell)
		}
		if rowHas {
			monthsCount++
		}
		row.HasAny = rowHas
		rows = append(rows, row)
	}

	for _, rsc := range resources {
		data.Totals = append(data.Totals, journalCellView{
			Resource:    rsc,
			Short:       rsc.Short(),
			Unit:        rsc.Unit(),
			Consumption: model.FormatValue(totals[rsc]),
			Has:         true,
		})
		data.BookTotals = append(data.BookTotals, journalTotalView{
			Label:       rsc.Section(),
			Consumption: model.FormatValue(totals[rsc]),
			Unit:        rsc.Unit(),
		})
	}
	data.MonthsCount = monthsCount
	data.HasMonths = monthsCount > 0

	// «Итоги» — та же сетка, что у начислений: месяц и колонки по ресурсам.
	// выбранному периоду журнала: строка на каждый месяц диапазона.
	// Здесь же можно менять суммы и отметки оплаты.
	data.MoneyRange = s.buildMoneyRange(obj, from, to, today, scope, false)
	// Ведомость начислений открывается той же страницей документа, что и остальные.
	data.MoneyRange.StatementHref = reportPreviewHref(obj.ID, "charges", scope, to, from, to)

	data.MoneyAny = data.MoneyRange.HasAny
	data.MoneyFromLabel = data.MoneyRange.FromLabel
	data.MoneyToLabel = data.MoneyRange.ToLabel
	// Ссылки внутри журнала сохраняют объект, диапазон и фильтр.
	hrefFor := func(f, t model.Period, sc string) string {
		q := url.Values{}
		q.Set("object", strconv.FormatInt(obj.ID, 10))
		q.Set("from", f.String())
		q.Set("to", t.String())
		if sc != "all" {
			q.Set("scope", sc)
		}
		return "/journal?" + q.Encode()
	}
	lastYear := model.Period{Year: today.Year - 1, Month: 1}
	lastYearEnd := model.Period{Year: today.Year - 1, Month: 12}
	data.From = from
	data.To = to
	data.FromValue = from.String()
	data.ToValue = to.String()
	data.FromLabel = from.Label()
	data.ToLabel = to.Label()
	switch {
	case from.Year == to.Year && from.Month == 1 && to.Month == 12:
		data.RangeLabel = strconv.Itoa(from.Year) + " год"
	case from == to:
		data.RangeLabel = from.Label()
	default:
		data.RangeLabel = from.Label() + " — " + to.Label()
	}
	data.Ranges = []journalRangeView{
		{Label: "Этот месяц", Href: hrefFor(today, today, scope), From: today, To: today},
		{Label: "Этот год", Href: hrefFor(thisYear(today), endYear(today), scope), From: thisYear(today), To: endYear(today)},
		{Label: "С начала года", Href: hrefFor(thisYear(today), today, scope), From: thisYear(today), To: today},
		{Label: "Прошлый год", Href: hrefFor(lastYear, lastYearEnd, scope), From: lastYear, To: lastYearEnd, Wide: true},
		{Label: "2 года", Href: hrefFor(lastYear, endYear(today), scope), From: lastYear, To: endYear(today), Wide: true},
	}
	for i := range data.Ranges {
		data.Ranges[i].Active = from == data.Ranges[i].From && to == data.Ranges[i].To
	}
	data.Object = obj
	data.Year = year
	data.PrevYear = year - 1
	data.NextYear = year + 1
	data.Rows = rows
	data.Resources = resources
	data.HasData = len(meters) > 0
	data.HasYear = hasYear
	data.Scope = scope
	data.ScopeAllHref = hrefFor(from, to, "all")
	data.ScopeWaterHref = hrefFor(from, to, "water")
	data.ScopeEnergyHref = hrefFor(from, to, "energy")

	// За один месяц печатаем «Сведения о показаниях», за период — ведомость.
	statementKind := "range"
	statementLink := reportPreviewHref(obj.ID, statementKind, scope, from, from, to)
	if from == to {
		statementKind = "month"
		statementLink = reportPreviewHref(obj.ID, statementKind, scope, from, from, to)
	}
	data.StatementHref = statementLink

	// Выгрузка: CSV — показания за период, PDF — тот же документ, что и
	// «Печать/отправить» у «Итогов» (за месяц — сведения, за период — ведомость).
	data.CSVHref = "/export.csv?" + url.Values{
		"object": {strconv.FormatInt(obj.ID, 10)},
		"from":   {from.String()},
		"to":     {to.String()},
	}.Encode()
	data.DocPDFHref = "/pdf?" + url.Values{
		"object": {strconv.FormatInt(obj.ID, 10)},
		"kind":   {statementKind},
		"scope":  {scope},
		"from":   {from.String()},
		"to":     {to.String()},
	}.Encode()

	// Таблица «Итоги» ссылается на лист показаний: собираем её после ссылки.
	data.JournalTable = buildJournalTable(rows, resources, data.Totals,
		monthsCount, data.RangeLabel, data.StatementHref)
	selected := today
	if shared.Year == year {
		selected = shared
	}
	rememberPeriod(w, selected)
	s.withObjectContext(w, r, &data.Base, obj.ID, "/journal")
	s.render(w, http.StatusOK, "journal", data)
}

func thisYear(today model.Period) model.Period { return model.Period{Year: today.Year, Month: 1} }
func endYear(today model.Period) model.Period  { return model.Period{Year: today.Year, Month: 12} }

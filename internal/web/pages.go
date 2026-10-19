package web

import (
	"fmt"
	"net/http"
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
		Debt:        m.Debt(),
		Paid:        m.PaidFlag,
		AdvancePaid: m.AdvancePaid,
		ShowAdvance: true,
		ShowBill:    true,
	}
}

func moneyRowsFrom(items map[model.Account]model.Money) []moneyRowView {
	out := make([]moneyRowView, 0, 3)
	for _, acc := range model.AccountOrder {
		m := items[acc]
		out = append(out, moneyRowViewFor(acc, m, "", ""))
	}
	return out
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

type entrySectionView struct {
	Resource model.Resource
	Title    string
	Unit     string
	Meters   []entryMeterView
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
		prev, _, hasPrev := report.PrevReading(series, period)
		if !hasPrev && m.StartValue > 0 {
			prev, hasPrev = m.StartValue, true
		}
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

	sections := make([]entrySectionView, 0, 3)
	sectionIdx := map[model.Resource]int{}
	filled, total := 0, 0
	var water, energy float64
	for _, m := range meters {
		v := views[m.ID]
		idx, ok := sectionIdx[m.Resource]
		if !ok {
			idx = len(sections)
			sectionIdx[m.Resource] = idx
			sections = append(sections, entrySectionView{
				Resource: m.Resource,
				Title:    m.Resource.Section(),
				Unit:     m.Resource.Unit(),
			})
		}
		sections[idx].Meters = append(sections[idx].Meters, *v)
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

	avail, _ := s.Store.AvailableMonths(obj.ID)
	nav := buildMonthNav(period, monthsMap(avail), func(p model.Period) string {
		return fmt.Sprintf("/?object=%d&m=%s", obj.ID, p.String())
	}, model.PeriodOf(time.Now().In(s.Loc)))

	base := s.base(obj.Title(), "home")
	base.Error = errMsg
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
		prev, _, hasPrev := report.PrevReading(readings[m.ID], period)
		if !hasPrev && m.StartValue > 0 {
			prev, hasPrev = m.StartValue, true
		}
		if hasPrev && val < prev {
			problems = append(problems, fmt.Sprintf("%s: показание %s меньше предыдущего %s",
				m.Title(), model.FormatValue(val), model.FormatValue(prev)))
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

type chartBar struct {
	Period string
	Label  string
	Value  float64
	Has    bool
	X      float64
	CX     float64
	Y      float64
	H      float64
	W      float64
}

type chartView struct {
	Kind       string
	Label      string
	Unit       string
	Bars       []chartBar
	Total      float64
	Max        float64
	HasData    bool
	WaterHref  string
	EnergyHref string
	IsWater    bool
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
	Chart         chartView
	Money         []moneyRowView
	MoneyAny      bool
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
}

func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	objects, err := s.Store.Objects(false)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Ошибка", "Не удалось прочитать объекты.")
		return
	}
	base := s.base("Журнал", "journal")
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
		for p := range od.Readings[m.ID] {
			if strings.HasPrefix(p, strconv.Itoa(year)) {
				active = append(active, m)
				break
			}
		}
	}
	meters = report.OrderedMeters(active)

	resources := []model.Resource{}
	for _, rsc := range model.ResourceOrder {
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
					hasReading = true
					if !hasReading || v > reading {
						reading = v
					}
					cell.Has = true
				}
				if m.IsSub() {
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

	chart := buildChart(od, year, r.URL.Query().Get("chart"), resources)
	chart.WaterHref = fmt.Sprintf("/journal?object=%d&year=%d&chart=water", obj.ID, year)
	chart.EnergyHref = fmt.Sprintf("/journal?object=%d&year=%d&chart=energy", obj.ID, year)

	var money []moneyRowView
	var moneyAny bool
	moneyTotals, _ := s.Store.MoneyTotals(obj.ID, model.Period{Year: year, Month: 1}, model.Period{Year: year, Month: 12})
	money = moneyRowsFrom(moneyTotals)
	for _, m := range money {
		if m.ChargedRaw != 0 || m.AdvanceRaw != 0 {
			moneyAny = true
		}
	}

	baseHref := "/journal?object=" + strconv.FormatInt(obj.ID, 10)
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
		{Label: "Этот год", Href: baseHref + "&from=" + thisYear(today).String() + "&to=" + endYear(today).String()},
		{Label: "С начала года", Href: baseHref + "&range=ytd"},
		{Label: "3 месяца", Href: baseHref + "&range=q3"},
		{Label: "Прошлый год", Href: baseHref + "&range=prev"},
		{Label: "2 года", Href: baseHref + "&range=2y"},
	}
	for i := range data.Ranges {
		data.Ranges[i].Active = strings.Contains(data.Ranges[i].Href, "from=") &&
			strings.Contains(data.Ranges[i].Href, "from="+from.String()) &&
			strings.Contains(data.Ranges[i].Href, "to="+to.String())
	}
	data.Object = obj
	data.Year = year
	data.PrevYear = year - 1
	data.NextYear = year + 1
	data.Rows = rows
	data.Resources = resources
	data.HasData = len(meters) > 0
	data.HasYear = hasYear
	data.Chart = chart
	data.Money = money
	data.MoneyAny = moneyAny
	selected := today
	if shared.Year == year {
		selected = shared
	}
	rememberPeriod(w, selected)
	s.withObjectContext(w, r, &data.Base, obj.ID, "/journal")
	s.render(w, http.StatusOK, "journal", data)
}

func buildChart(od report.ObjectData, year int, kind string, resources []model.Resource) chartView {
	hasWater := false
	hasEnergy := false
	for _, r := range resources {
		if r.IsWater() {
			hasWater = true
		} else if r == model.Energy {
			hasEnergy = true
		}
	}
	if kind != "water" && kind != "energy" {
		if hasWater {
			kind = "water"
		} else {
			kind = "energy"
		}
	}
	if kind == "water" && !hasWater {
		kind = "energy"
	}
	if kind == "energy" && !hasEnergy {
		kind = "water"
	}
	cv := chartView{Kind: kind, IsWater: kind == "water"}
	if kind == "water" {
		cv.Label = "Вода"
		cv.Unit = "куб.м"
	} else {
		cv.Label = "Электроэнергия"
		cv.Unit = "кВт·ч"
	}

	values := make([]float64, 12)
	for i := 0; i < 12; i++ {
		p, _ := model.NewPeriod(year, i+1)
		for _, m := range od.Meters {
			if m.IsSub() {
				continue
			}
			match := (kind == "water" && m.Resource.IsWater()) || (kind == "energy" && m.Resource == model.Energy)
			if !match {
				continue
			}
			if cons, ok := od.ConsumptionFor(m, p); ok {
				values[i] = model.Round3(values[i] + cons)
			}
		}
	}
	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
		cv.Total = model.Round3(cv.Total + v)
	}
	cv.Max = max
	cv.HasData = max > 0

	const width = 720.0
	const height = 190.0
	const padLeft = 8.0
	const padRight = 8.0
	const padTop = 14.0
	const padBottom = 26.0
	plotW := width - padLeft - padRight
	step := plotW / 12
	barW := step * 0.52
	for i, v := range values {
		p, _ := model.NewPeriod(year, i+1)
		h := 2.0
		if max > 0 && v > 0 {
			h = (height - padTop - padBottom) * v / max
			if h < 2 {
				h = 2
			}
		}
		cv.Bars = append(cv.Bars, chartBar{
			Period: p.String(),
			Label:  p.MonthShort(),
			Value:  v,
			Has:    v > 0,
			X:      padLeft + float64(i)*step + (step-barW)/2,
			CX:     padLeft + float64(i)*step + step/2,
			Y:      height - padBottom - h,
			H:      h,
			W:      barW,
		})
	}
	return cv
}

func thisYear(today model.Period) model.Period { return model.Period{Year: today.Year, Month: 1} }
func endYear(today model.Period) model.Period  { return model.Period{Year: today.Year, Month: 12} }

// Package report строит данные для экранов и документов.
package report

import (
	"sort"
	"time"

	"indextro/internal/model"
)

// ObjectData — объект вместе с приборами и всеми показаниями.
type ObjectData struct {
	Object   model.Object
	Meters   []model.Meter
	Readings map[int64]map[string]float64 // meterID -> period -> value
}

// MonthRow — строка месячного отчёта по одному прибору.
type MonthRow struct {
	Meter    model.Meter
	Prev     float64
	Curr     float64
	Diff     float64
	HasPrev  bool
	HasCurr  bool
	HasDiff  bool
	IsSub    bool // строка суб-счётчика
	Net      bool // расход уменьшен на суб-счётчики
	SubCount int
	SubTotal float64
	PrevText string
	Note     string
}

// MonthSection — раздел отчёта по ресурсу.
type MonthSection struct {
	Resource model.Resource
	Title    string
	Unit     string
	Rows     []MonthRow
	Total    float64
	Filled   int
}

// Month — сведения за месяц.
type Month struct {
	Object      model.Object
	Period      model.Period
	Scope       string
	Generated   time.Time
	Sections    []MonthSection
	HasData     bool
	HasReadings bool
	HasSub      bool
	Missing     int
	Filled      int
	Total       int
}

// RangeCell — расход по ресурсу за месяц.
type RangeCell struct {
	Resource    model.Resource
	Consumption float64
	Has         bool
}

// RangeRow — строка ведомости (месяц).
type RangeRow struct {
	Period model.Period
	Cells  []RangeCell
}

// MeterTotal — итог по прибору за период.
type MeterTotal struct {
	Meter      model.Meter
	Total      float64
	HasTotal   bool
	IsSub      bool
	LastValue  float64
	HasLast    bool
	LastPeriod model.Period
}

// Range — ведомость за произвольный период.
type Range struct {
	Object         model.Object
	From, To       model.Period
	Scope          string
	Generated      time.Time
	Resources      []model.Resource
	Rows           []RangeRow
	MeterTotals    []MeterTotal
	ResourceTotals map[model.Resource]float64
	HasData        bool
}

// Scoped сообщает, входит ли ресурс в выбранный набор.
func Scoped(r model.Resource, scope string) bool {
	switch scope {
	case "water":
		return r.IsWater()
	case "energy":
		return r == model.Energy
	default:
		return true
	}
}

func scopedMeters(ms []model.Meter, scope string) []model.Meter {
	out := make([]model.Meter, 0, len(ms))
	for _, m := range ms {
		if Scoped(m.Resource, scope) {
			out = append(out, m)
		}
	}
	return out
}

// PrevReading ищет ближайшее показание до периода p.
func PrevReading(series map[string]float64, p model.Period) (float64, model.Period, bool) {
	var best model.Period
	var val float64
	found := false
	for key, v := range series {
		pp, err := model.ParsePeriod(key)
		if err != nil || !pp.Before(p) {
			continue
		}
		if !found || best.Before(pp) {
			best, val, found = pp, v, true
		}
	}
	return val, best, found
}

// LastReadingUpto ищет последнее показание не позже периода p.
func LastReadingUpto(series map[string]float64, p model.Period) (float64, model.Period, bool) {
	var best model.Period
	var val float64
	found := false
	for key, v := range series {
		pp, err := model.ParsePeriod(key)
		if err != nil || pp.After(p) {
			continue
		}
		if !found || best.Before(pp) {
			best, val, found = pp, v, true
		}
	}
	return val, best, found
}

// Children возвращает прямых суб-счётчиков прибора.
func (od ObjectData) Children(parentID int64) []model.Meter {
	var out []model.Meter
	for _, m := range od.Meters {
		if m.ParentID == parentID {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// HasChildren сообщает, есть ли у прибора суб-счётчики.
func (od ObjectData) HasChildren(m model.Meter) bool {
	for _, other := range od.Meters {
		if other.ParentID == m.ID {
			return true
		}
	}
	return false
}

// OwnDiff — расход по самому прибору, без вычета суб-счётчиков.
func (od ObjectData) OwnDiff(m model.Meter, p model.Period) (float64, bool) {
	series := od.Readings[m.ID]
	curr, hasCurr := series[p.String()]
	if !hasCurr {
		return 0, false
	}
	prev, _, hasPrev := PrevReading(series, p)
	if !hasPrev && m.StartValue > 0 {
		prev, hasPrev = m.StartValue, true
	}
	if !hasPrev || curr < prev {
		return 0, false
	}
	return model.Round3(curr - prev), true
}

// NetDiff — расход с вычетом суб-счётчиков.
// Возвращает расход, признак наличия, сумму вычета и число суб-счётчиков.
func (od ObjectData) NetDiff(m model.Meter, p model.Period) (float64, bool, float64, int) {
	own, has := od.OwnDiff(m, p)
	if !has {
		return 0, false, 0, 0
	}
	if m.IsSub() {
		return own, true, 0, 0
	}
	// Дублирующие счётчики не вычитаются: они измеряют тот же расход.
	return own, true, 0, 0
}

// OrderedMeters возвращает приборы в порядке «главный, затем его суб-счётчики».
func OrderedMeters(ms []model.Meter) []model.Meter {
	ids := map[int64]bool{}
	for _, m := range ms {
		ids[m.ID] = true
	}
	children := map[int64][]model.Meter{}
	var roots []model.Meter
	for _, m := range ms {
		if m.ParentID != 0 && ids[m.ParentID] {
			children[m.ParentID] = append(children[m.ParentID], m)
		} else {
			roots = append(roots, m)
		}
	}
	sortMeters := func(list []model.Meter) {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Sort != list[j].Sort {
				return list[i].Sort < list[j].Sort
			}
			return list[i].ID < list[j].ID
		})
	}
	sortMeters(roots)
	for id := range children {
		sortMeters(children[id])
	}
	out := make([]model.Meter, 0, len(ms))
	seen := map[int64]bool{}
	var add func(m model.Meter)
	add = func(m model.Meter) {
		if seen[m.ID] {
			return
		}
		seen[m.ID] = true
		out = append(out, m)
		for _, c := range children[m.ID] {
			add(c)
		}
	}
	for _, m := range roots {
		add(m)
	}
	for _, m := range ms {
		if !seen[m.ID] {
			add(m)
		}
	}
	return out
}

// MonthReport собирает сведения за месяц.
func MonthReport(od ObjectData, p model.Period, scope string, now time.Time) Month {
	rep := Month{Object: od.Object, Period: p, Scope: scope, Generated: now}
	meters := OrderedMeters(scopedMeters(od.Meters, scope))
	for _, m := range meters {
		series := od.Readings[m.ID]
		curr, hasCurr := series[p.String()]
		if m.Archived && !hasCurr {
			continue
		}
		prev, prevP, hasPrev := PrevReading(series, p)
		if !hasPrev && m.StartValue > 0 {
			prev, hasPrev = m.StartValue, true
		}

		row := MonthRow{Meter: m, Curr: curr, HasCurr: hasCurr, HasPrev: hasPrev, IsSub: m.IsSub()}
		if m.IsSub() {
			rep.HasSub = true
		}
		if hasPrev {
			row.Prev = prev
			row.PrevText = prevP.Label()
		}
		if hasCurr && hasPrev && curr >= prev {
			row.Diff = model.Round3(curr - prev)
			row.HasDiff = true
		}
		if !m.IsSub() {
			if net, ok, subSum, subCount := od.NetDiff(m, p); ok {
				row.Diff = net
				row.HasDiff = true
				row.SubTotal = subSum
				row.SubCount = subCount
				row.Net = subCount > 0
			}
		}

		idx := sectionIndex(&rep, m.Resource)
		rep.Sections[idx].Rows = append(rep.Sections[idx].Rows, row)
		if hasCurr {
			rep.Sections[idx].Filled++
			rep.HasReadings = true
			rep.Filled++
		} else {
			rep.Missing++
		}
		rep.Total++
		if row.HasDiff && !m.IsSub() {
			rep.Sections[idx].Total = model.Round3(rep.Sections[idx].Total + row.Diff)
			rep.HasData = true
		}
	}
	sections := rep.Sections[:0]
	for _, s := range rep.Sections {
		if len(s.Rows) > 0 {
			sections = append(sections, s)
		}
	}
	rep.Sections = sections
	return rep
}

func sectionIndex(rep *Month, r model.Resource) int {
	for i := range rep.Sections {
		if rep.Sections[i].Resource == r {
			return i
		}
	}
	rep.Sections = append(rep.Sections, MonthSection{
		Resource: r,
		Title:    r.Section(),
		Unit:     r.Unit(),
	})
	return len(rep.Sections) - 1
}

// RangeReport собирает ведомость за период [from, to].
func RangeReport(od ObjectData, from, to model.Period, scope string, now time.Time) Range {
	rep := Range{
		Object:         od.Object,
		From:           from,
		To:             to,
		Scope:          scope,
		Generated:      now,
		ResourceTotals: map[model.Resource]float64{},
	}
	if to.Before(from) {
		return rep
	}
	meters := OrderedMeters(scopedMeters(od.Meters, scope))
	rep.Resources = []model.Resource{}
	for _, m := range meters {
		if m.IsSub() {
			continue
		}
		if !containsResource(rep.Resources, m.Resource) {
			rep.Resources = append(rep.Resources, m.Resource)
		}
	}
	if len(rep.Resources) == 0 {
		return rep
	}
	ordered := make([]model.Resource, 0, len(rep.Resources))
	for _, r := range model.ResourceOrder {
		if containsResource(rep.Resources, r) {
			ordered = append(ordered, r)
		}
	}
	rep.Resources = ordered

	type agg struct {
		sum float64
		has bool
	}
	cells := map[string]map[model.Resource]*agg{}
	totals := map[int64]float64{}
	hasTotals := map[int64]bool{}
	lastVals := map[int64]MeterTotal{}

	for _, m := range meters {
		series := od.Readings[m.ID]
		for each := from; !each.After(to); each = each.Next() {
			var v float64
			var ok bool
			if m.IsSub() {
				v, ok = od.OwnDiff(m, each)
			} else {
				v, ok, _, _ = od.NetDiff(m, each)
			}
			if !ok {
				continue
			}
			// Суб-счётчик уже вычтен из расхода основного прибора:
			// в сумму по ресурсу он не входит, но его итог важен для справки.
			if m.IsSub() {
				totals[m.ID] = model.Round3(totals[m.ID] + v)
				hasTotals[m.ID] = true
				continue
			}
			key := each.String()
			if cells[key] == nil {
				cells[key] = map[model.Resource]*agg{}
			}
			a := cells[key][m.Resource]
			if a == nil {
				a = &agg{}
				cells[key][m.Resource] = a
			}
			a.sum = model.Round3(a.sum + v)
			a.has = true
			totals[m.ID] = model.Round3(totals[m.ID] + v)
			hasTotals[m.ID] = true
		}
		last, lastP, hasLast := LastReadingUpto(series, to)
		lastVals[m.ID] = MeterTotal{
			Meter:      m,
			IsSub:      m.IsSub(),
			LastValue:  last,
			HasLast:    hasLast,
			LastPeriod: lastP,
		}
	}

	for each := from; !each.After(to); each = each.Next() {
		row := RangeRow{Period: each}
		for _, r := range rep.Resources {
			c := RangeCell{Resource: r}
			if a := cells[each.String()][r]; a != nil && a.has {
				c.Consumption = a.sum
				c.Has = true
				rep.ResourceTotals[r] = model.Round3(rep.ResourceTotals[r] + a.sum)
				rep.HasData = true
			}
			row.Cells = append(row.Cells, c)
		}
		rep.Rows = append(rep.Rows, row)
	}

	rep.MeterTotals = make([]MeterTotal, 0, len(meters))
	for _, m := range meters {
		mt := lastVals[m.ID]
		mt.Total = totals[m.ID]
		mt.HasTotal = hasTotals[m.ID]
		rep.MeterTotals = append(rep.MeterTotals, mt)
	}
	return rep
}

func containsResource(list []model.Resource, r model.Resource) bool {
	for _, v := range list {
		if v == r {
			return true
		}
	}
	return false
}

// PeriodBounds — границы периода в днях.
func PeriodBounds(from, to model.Period) (time.Time, time.Time) {
	start := time.Date(from.Year, time.Month(from.Month), 1, 0, 0, 0, 0, time.Local)
	end := time.Date(to.Year, time.Month(to.Month)+1, 0, 0, 0, 0, 0, time.Local)
	return start, end
}

// Status — состояние ввода за месяц для списка объектов.
type Status struct {
	Total  int
	Filled int
}

// Complete сообщает, что все приборы заполнены.
func (s Status) Complete() bool { return s.Total > 0 && s.Filled == s.Total }

// StatusFor считает заполненность месяца по объекту.
func StatusFor(od ObjectData, p model.Period) Status {
	var st Status
	for _, m := range od.Meters {
		if m.Archived {
			continue
		}
		st.Total++
		if _, ok := od.Readings[m.ID][p.String()]; ok {
			st.Filled++
		}
	}
	return st
}

// ConsumptionFor — расход для журнала: чистый для главного, свой для суб-счётчика.
func (od ObjectData) ConsumptionFor(m model.Meter, p model.Period) (float64, bool) {
	if m.IsSub() {
		return od.OwnDiff(m, p)
	}
	v, ok, _, _ := od.NetDiff(m, p)
	return v, ok
}

// TopLevel оставляет только самостоятельные приборы.
func TopLevel(ms []model.Meter) []model.Meter {
	out := make([]model.Meter, 0, len(ms))
	for _, m := range ms {
		if !m.IsSub() {
			out = append(out, m)
		}
	}
	return out
}

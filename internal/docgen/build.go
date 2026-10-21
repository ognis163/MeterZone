package docgen

import (
	"fmt"
	"strings"
	"time"

	"indextro/internal/model"
	"indextro/internal/report"
)

// Month собирает «Сведения о показаниях» за месяц.
func Month(rep report.Month) Document {
	d := Document{
		Kind:      "month",
		Title:     monthTitle(rep.Scope),
		Subtitle:  "за " + rep.Period.Accusative() + " г.",
		Generated: rep.Generated,
		Signature: true,
	}
	d.Generated = generatedAt(d.Generated)
	d.Meta = requisites(rep.Object, rep.Scope, rep.Period, true)

	if len(rep.Sections) == 0 || !rep.HasReadings {
		d.Empty = "Показания за выбранный месяц не вносились."
		return d
	}

	for _, s := range rep.Sections {
		unit := s.Unit
		if unit == "" {
			unit = s.Resource.Unit()
		}
		sec := Section{
			Title:      s.Resource.Section(),
			PlainTitle: true,
			Columns: []Column{
				{"Место\nустановки\nприбора учета", 38, Left},
				{"Марка\nприбора учета", 32, Left},
				{"Заводской\n№", 30, Left},
				{"Предыдущее", 22, Right},
				{"Текущее", 22, Right},
				{"Расход,\n" + unit, 22, Right},
			},
		}
		for _, r := range s.Rows {
			place := r.Meter.Title()
			if r.IsSub && !isDuplicate(place) {
				place += " · дубль"
			}
			sec.Rows = append(sec.Rows, Row{Cells: []Cell{
				{Text: place},
				{Text: dash(r.Meter.Brand), Muted: r.Meter.Brand == ""},
				{Text: dash(r.Meter.Serial), Muted: r.Meter.Serial == ""},
				value(r.Prev, r.HasPrev),
				value(r.Curr, r.HasCurr),
				value(r.Diff, r.HasDiff),
			}})
		}
		d.Sections = append(d.Sections, sec)
	}
	return d
}

// Range собирает «Ведомость показаний» за период.
func Range(rep report.Range) Document {
	d := Document{
		Kind:       "range",
		Title:      rangeTitle(rep.Scope),
		Subtitle:   rangeSubtitle(rep.From, rep.To),
		Generated:  rep.Generated,
		Signature:  true,
		MetaGap:    10,
		SectionGap: 10,
	}
	d.Generated = generatedAt(d.Generated)
	d.Meta = requisites(rep.Object, rep.Scope, rep.From, false)

	if !rep.HasData || len(rep.Resources) == 0 || len(rep.Rows) == 0 {
		d.Empty = "За выбранный период данных нет."
		return d
	}

	d.Sections = append(d.Sections, readingsSection(rep))
	if sec, ok := metersSection(rep); ok {
		d.Sections = append(d.Sections, sec)
	}
	return d
}

// readingsSection — таблица «месяц × ресурс»: показания и расход.
func readingsSection(rep report.Range) Section {
	sec := Section{Title: "Показания", PlainTitle: true}
	colW := (ContentW - 36) / float64(len(rep.Resources)*2)
	sec.Columns = append(sec.Columns, Column{"Месяц", 36, Left})
	for _, r := range rep.Resources {
		sec.Columns = append(sec.Columns,
			Column{resourceHead(r), colW, Right},
			Column{"расход,\n" + r.Unit(), colW, Right},
		)
	}

	for _, row := range rep.Rows {
		cells := []Cell{{Text: row.Period.Label()}}
		for _, c := range row.Cells {
			cells = append(cells,
				value(c.Reading, c.HasReading),
				value(c.Consumption, c.Has),
			)
		}
		sec.Rows = append(sec.Rows, Row{Cells: cells})
	}

	sec.Total = []Cell{{Text: "Итого"}}
	for _, r := range rep.Resources {
		sec.Total = append(sec.Total,
			Cell{Text: "—", Muted: true},
			Cell{Text: model.FormatValue(rep.ResourceTotals[r])},
		)
	}
	return sec
}

// metersSection — сводка по приборам: последнее показание и расход.
func metersSection(rep report.Range) (Section, bool) {
	if len(rep.MeterTotals) == 0 {
		return Section{}, false
	}
	sec := Section{
		Title:      "Приборы учёта",
		PlainTitle: true,
		Columns: []Column{
			{"Прибор", 70, Left},
			{"Ресурс", 40, Left},
			{"Показание", 28, Right},
			{"Расход", 28, Right},
		},
	}
	for _, mt := range rep.MeterTotals {
		label := mt.Meter.LabelShort()
		if mt.IsSub && !isDuplicate(label) {
			label += " · дубль"
		}
		if mt.Meter.Archived {
			label += " · не активен"
		}
		sec.Rows = append(sec.Rows, Row{Cells: []Cell{
			{Text: label},
			{Text: mt.Meter.Resource.Label()},
			value(mt.LastValue, mt.HasLast),
			value(mt.Total, mt.HasTotal),
		}})
	}
	return sec, true
}

// requisites — шапка документа: собственник, адрес, лицевые счета, месяц.
func requisites(o model.Object, scope string, p model.Period, withPeriod bool) []Meta {
	meta := []Meta{{Label: "Собственник", Lines: []string{dash(o.Owner)}}}
	if strings.TrimSpace(o.Address) != "" {
		meta = append(meta, Meta{Label: "Адрес объекта", Lines: []string{o.Address}})
	}

	var accounts []string
	if scope != "energy" && strings.TrimSpace(o.AccountWater) != "" {
		accounts = append(accounts, "Лицевой счёт (вода): "+o.AccountWater)
	}
	if scope != "water" && strings.TrimSpace(o.AccountEnergy) != "" {
		accounts = append(accounts, "Лицевой счёт (электро): "+o.AccountEnergy)
	}
	if len(accounts) > 0 {
		meta = append(meta, Meta{Label: "Реквизиты", Lines: accounts})
	}

	if withPeriod {
		meta = append(meta, Meta{Label: "Расчётный месяц", Lines: []string{p.Label()}})
	}
	return meta
}

func rangeSubtitle(from, to model.Period) string {
	if from == to {
		return "за " + from.Accusative() + " г."
	}
	return fmt.Sprintf("за период с %s по %s", from.Genitive(), to.Accusative())
}

// monthTitle называет документ по выбранному набору ресурсов.
func monthTitle(scope string) string {
	switch scope {
	case "water":
		return "Сведения о показаниях воды"
	case "energy":
		return "Сведения о показаниях электроэнергии"
	}
	return "Сведения о показаниях"
}

// rangeTitle — то же для ведомости за период.
func rangeTitle(scope string) string {
	switch scope {
	case "water":
		return "Ведомость показаний воды"
	case "energy":
		return "Ведомость показаний электроэнергии"
	}
	return "Ведомость показаний"
}

// value форматирует число или прочерк, если значения нет.
func value(v float64, ok bool) Cell {
	if !ok {
		return Cell{Text: "—", Muted: true}
	}
	return Cell{Text: model.FormatValue(v)}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// isDuplicate сообщает, что в подписи уже сказано про дублирующий счётчик.
func isDuplicate(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "дубль") || strings.Contains(s, "суб")
}

func generatedAt(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// resourceHead — короткий заголовок колонки ресурса. Единицу измерения
// печатаем только в столбце «расход», чтобы не дублировать её.
func resourceHead(r model.Resource) string {
	switch r {
	case model.WaterCold:
		return "ХВС"
	case model.WaterHot:
		return "ГВС"
	}
	return "Электроэнергия"
}

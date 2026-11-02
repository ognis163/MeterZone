package web

import "indextro/internal/model"

// monthCellView — месяц в горизонтальной полосе (десктоп).
type monthCellView struct {
	Period   model.Period
	Label    string
	Href     string
	Selected bool
	HasData  bool
	Disabled bool
}

// monthStripView — полоса месяцев для ПК.
type monthStripView struct {
	Year      int
	Month     model.Period
	MonthName string
	Cells     []monthCellView
	PrevHref  string
	NextHref  string
	IsCurrent bool
	Years     []monthYearItem
}

// monthNavView — общий блок выбора месяца: полоса на ПК, стрелки на телефоне.
type monthNavView struct {
	Month     model.Period
	PrevHref  string
	NextHref  string
	IsCurrent bool
	Strip     monthStripView
}

// buildMonthStrip собирает 12 месяцев выбранного года.
// href задаёт ссылку на месяц, hasData отмечает заполненные,
// disableEmpty запрещает выбор пустых (для отчётов).
func buildMonthStrip(p model.Period, hasData map[string]bool, href func(model.Period) string,
	disableEmpty bool, today model.Period) monthStripView {

	view := monthStripView{
		Year:      p.Year,
		Month:     p,
		MonthName: p.Label(),
		PrevHref:  href(p.Add(-12)),
		NextHref:  href(p.Add(12)),
		IsCurrent: p == today,
	}
	// Годы для колеса на телефоне: пять назад и пять вперёд.
	for y := p.Year - 5; y <= p.Year+5; y++ {
		cell, err := model.NewPeriod(y, p.Month)
		if err != nil {
			continue
		}
		view.Years = append(view.Years, monthYearItem{
			Year:    y,
			Href:    href(cell),
			Current: y == p.Year,
		})
	}
	for m := 1; m <= 12; m++ {
		cell, err := model.NewPeriod(p.Year, m)
		if err != nil {
			continue
		}
		key := cell.String()
		has := hasData[key]
		disabled := disableEmpty && !has
		hv := ""
		if !disabled {
			hv = href(cell)
		}
		view.Cells = append(view.Cells, monthCellView{
			Period:   cell,
			Label:    cell.MonthShort(),
			Href:     hv,
			Selected: cell == p,
			HasData:  has,
			Disabled: disabled,
		})
	}
	return view
}

func buildMonthNav(p model.Period, hasData map[string]bool, href func(model.Period) string,
	today model.Period) monthNavView {
	return monthNavView{
		Month:     p,
		PrevHref:  href(p.Prev()),
		NextHref:  href(p.Next()),
		IsCurrent: p == today,
		Strip:     buildMonthStrip(p, hasData, href, false, today),
	}
}

func monthsMap(list []model.Period) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, p := range list {
		out[p.String()] = true
	}
	return out
}

// monthYearItem — год в списке выбора года на телефоне.
type monthYearItem struct {
	Year    int
	Href    string
	Current bool
}

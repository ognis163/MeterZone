package web

import "indextro/internal/model"

// periodRibbonNav — выбор года и горизонтальная лента месяцев вокруг
// выбранного месяца, плюс телефонный выбор месяца списком.
type periodRibbonNav struct {
	Year       int
	Years      []yearOptionView
	Ribbon     []ribbonMonthView
	PrevRibbon string
	NextRibbon string
	Pick       periodPick
}

// periodPickMonth — строка списка выбора месяца.
type periodPickMonth struct {
	Label   string // «Ноябрь»
	Year    int    // 2026
	Href    string
	Current bool
}

// periodPick — выбор месяца одним полем: кнопка с подписью и прокручиваемый
// список месяцев. На телефоне заменяет кнопку года и ленту месяцев.
type periodPick struct {
	Label  string // «Ноябрь 2026»
	Months []periodPickMonth
}

// buildPeriodPick собирает список вокруг якорного месяца: два года назад и
// год вперёд. current помечает выбранный месяц — на нём список открывается.
func buildPeriodPick(anchor, current model.Period, label string, href func(model.Period) string) periodPick {
	if !anchor.Valid() {
		anchor = model.PeriodNow(nil)
	}
	pick := periodPick{Label: label}
	if pick.Label == "" {
		pick.Label = anchor.Label()
	}
	for _, p := range model.MonthsBetween(anchor.Add(-24), anchor.Add(12)) {
		pick.Months = append(pick.Months, periodPickMonth{
			Label:   p.MonthName(),
			Year:    p.Year,
			Href:    href(p),
			Current: current.Valid() && p == current,
		})
	}
	return pick
}

// buildPeriodRibbon собирает ленту из пяти месяцев выбранного года так,
// чтобы текущий месяц стоял в середине (у краёв года лента прижимается).
// Годы для списка берутся вокруг сегодняшнего года, как в журнале.
func buildPeriodRibbon(sel, today model.Period, href func(model.Period) string) periodRibbonNav {
	nav := periodRibbonNav{
		Year:       sel.Year,
		PrevRibbon: href(sel.Prev()),
		NextRibbon: href(sel.Next()),
		Pick:       buildPeriodPick(sel, sel, "", href),
	}

	start := sel.Month - 2
	if start < 1 {
		start = 1
	}
	if start > 8 {
		start = 8
	}
	for i := 0; i < 5; i++ {
		p := model.Period{Year: sel.Year, Month: start + i}
		nav.Ribbon = append(nav.Ribbon, ribbonMonthView{
			Label:   p.MonthShort(),
			Href:    href(p),
			Current: p == sel,
		})
	}

	for y := today.Year + 1; y >= today.Year-5; y-- {
		nav.Years = append(nav.Years, yearOptionView{
			Year:    y,
			Href:    href(model.Period{Year: y, Month: sel.Month}),
			Current: y == sel.Year,
		})
	}
	return nav
}

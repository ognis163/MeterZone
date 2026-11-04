package web

import "indextro/internal/model"

// periodRibbonNav — выбор года и горизонтальная лента месяцев вокруг
// выбранного месяца. Используется на главной странице.
type periodRibbonNav struct {
	Year       int
	Years      []yearOptionView
	Ribbon     []ribbonMonthView
	PrevRibbon string
	NextRibbon string
}

// buildPeriodRibbon собирает ленту из пяти месяцев выбранного года так,
// чтобы текущий месяц стоял в середине (у краёв года лента прижимается).
// Годы для списка берутся вокруг сегодняшнего года, как в журнале.
func buildPeriodRibbon(sel, today model.Period, href func(model.Period) string) periodRibbonNav {
	nav := periodRibbonNav{
		Year:       sel.Year,
		PrevRibbon: href(sel.Prev()),
		NextRibbon: href(sel.Next()),
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

package web

import (
	"net/url"
	"strconv"

	"indextro/internal/model"
)

// ------------------------------------------------------------------ шапка журнала

type yearOptionView struct {
	Year    int
	Href    string
	Current bool
}

type ribbonMonthView struct {
	Label   string
	Href    string
	Current bool
	Has     bool
}

func journalHref(objID int64, from, to model.Period, scope string, dupOn bool) string {
	q := url.Values{}
	q.Set("object", strconv.FormatInt(objID, 10))
	q.Set("from", from.String())
	q.Set("to", to.String())
	if scope != "all" {
		q.Set("scope", scope)
	}
	if dupOn {
		q.Set("dup", "1")
	}
	return "/journal?" + q.Encode()
}

// buildJournalNav собирает выбор года, ленту месяцев и режимы периода.
func buildJournalNav(objID int64, scope string, from, to model.Period, year int,
	today model.Period, dupOn bool, monthsWithData map[string]bool) (
	years []yearOptionView, ribbon []ribbonMonthView,
	monthHref, yearHref, twoHref, prevRibbon, nextRibbon string,
	modeMonth, modeYear, modeTwo bool) {

	yearHref = journalHref(objID, model.Period{Year: year, Month: 1}, model.Period{Year: year, Month: 12}, scope, dupOn)
	twoHref = journalHref(objID, model.Period{Year: year - 1, Month: 1}, model.Period{Year: year, Month: 12}, scope, dupOn)

	sel := model.Period{Year: year, Month: 1}
	switch {
	case from == to && from.Year == year:
		sel = from
	case today.Year == year:
		sel = today
	}
	monthHref = journalHref(objID, sel, sel, scope, dupOn)

	start := sel.Month - 2
	if start < 1 {
		start = 1
	}
	if start > 8 {
		start = 8
	}
	for i := 0; i < 5; i++ {
		p := model.Period{Year: year, Month: start + i}
		ribbon = append(ribbon, ribbonMonthView{
			Label:   p.MonthShort(),
			Href:    journalHref(objID, p, p, scope, dupOn),
			Current: p == sel,
			Has:     monthsWithData[p.String()],
		})
	}
	prev := sel
	if prev.Month > 1 {
		prev.Month--
	}
	next := sel
	if next.Month < 12 {
		next.Month++
	}
	prevRibbon = journalHref(objID, prev, prev, scope, dupOn)
	nextRibbon = journalHref(objID, next, next, scope, dupOn)

	for y := today.Year + 1; y >= today.Year-5; y-- {
		years = append(years, yearOptionView{
			Year:    y,
			Href:    journalHref(objID, model.Period{Year: y, Month: 1}, model.Period{Year: y, Month: 12}, scope, dupOn),
			Current: y == year,
		})
	}

	modeMonth = from == to
	modeYear = from.Year == to.Year && from.Month == 1 && to.Month == 12
	modeTwo = from.Year == to.Year-1 && from.Month == 1 && to.Month == 12
	return
}

// ------------------------------------------------------------------ «Итоги»

// journalResRow — строка «Итогов» за один месяц.
type journalResRow struct {
	Label       string
	Reading     string
	Consumption string
	Unit        string
}

// journalGroupView — группа колонок одного ресурса в ведомости.
type journalGroupView struct {
	Name string
	Dup  bool
}

// journalYearRow — строка ведомости: месяц и значения по группам.
type journalYearRow struct {
	Label   string
	Href    string
	Current bool
	Empty   bool
	Groups  [][]string
}

// journalYearView — ведомость по месяцам.
type journalYearView struct {
	Groups []journalGroupView
	Rows   []journalYearRow
	Foot   [][]string
}

func buildMonthRows(rows []journalRowView) []journalResRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]journalResRow, 0, len(rows[0].Cells))
	for _, c := range rows[0].Cells {
		out = append(out, journalResRow{
			Label:       c.Short,
			Reading:     pickValue(c.Reading),
			Consumption: pickValue(c.Consumption),
			Unit:        c.Unit,
		})
	}
	return out
}

func buildJournalYear(rows []journalRowView, resources []model.Resource,
	totals []journalCellView, dupOn bool) journalYearView {

	v := journalYearView{}
	dupCol := make([]bool, len(resources))
	if dupOn {
		for i := range resources {
			for _, row := range rows {
				if i < len(row.Cells) && row.Cells[i].HasDup {
					dupCol[i] = true
					break
				}
			}
		}
	}
	for i, r := range resources {
		v.Groups = append(v.Groups, journalGroupView{Name: r.Short(), Dup: dupCol[i]})
	}
	for _, row := range rows {
		jr := journalYearRow{Label: row.Label, Href: row.Href, Current: row.IsCurrent, Empty: !row.HasAny}
		for i, c := range row.Cells {
			g := []string{pickValue(c.Reading), pickValue(c.Consumption)}
			if i < len(dupCol) && dupCol[i] {
				g = append(g, pickValue(c.DupReading))
			}
			jr.Groups = append(jr.Groups, g)
		}
		v.Rows = append(v.Rows, jr)
	}
	if len(totals) > 0 {
		for i, c := range totals {
			g := []string{"—", "—"}
			if c.Consumption != "" {
				g[1] = c.Consumption
			}
			if i < len(dupCol) && dupCol[i] {
				g = append(g, "—")
			}
			v.Foot = append(v.Foot, g)
		}
	}
	return v
}

// ------------------------------------------------------------------ «Начисления»

// moneyResRow — строка начислений за месяц: ресурс, аванс, счёт, итого.
type moneyResRow struct {
	Label   string
	Advance string
	Invoice string
	Total   string
}

// moneyMonthRowView — строка начислений по месяцам (сводка за период).
type moneyMonthRowView struct {
	Label string
	Href  string
	Cells []string
	Total string
}

// moneyYearView — начисления по месяцам: колонки ресурсов и итог.
type moneyYearView struct {
	Accounts []string
	Rows     []moneyMonthRowView
	Foot     []string
}

// buildMoneyViews собирает обе формы таблицы начислений: за месяц — строки
// по ресурсам, за период — сводка по месяцам.
func buildMoneyViews(t moneyTableView, objID int64, scope string, dupOn bool) (
	res []moneyResRow, total moneyResRow, year moneyYearView) {

	total.Label = "Итого"

	order := []string{}
	short := map[string]string{}
	index := map[string]int{}
	for _, m := range t.Months {
		for _, ln := range m.Lines {
			if _, ok := short[ln.Account]; !ok {
				index[ln.Account] = len(order)
				order = append(order, ln.Account)
				short[ln.Account] = ln.Short
			}
		}
	}
	accSum := make([]float64, len(order))

	for _, m := range t.Months {
		row := moneyMonthRowView{Label: m.Label}
		if p, err := model.ParsePeriod(m.Period); err == nil {
			row.Href = journalHref(objID, p, p, scope, dupOn)
		}
		vals := make([]float64, len(order))
		var rowSum float64
		for _, ln := range m.Lines {
			var v float64
			if ln.Advance != nil {
				v += ln.Advance.sum()
			}
			if ln.Invoice != nil {
				v += ln.Invoice.sum()
			}
			i := index[ln.Account]
			vals[i] = v
			accSum[i] += v
			rowSum += v
		}
		row.Cells = make([]string, len(order))
		for i := range vals {
			row.Cells[i] = moneyOrDash(vals[i])
		}
		row.Total = moneyOrDash(rowSum)
		year.Rows = append(year.Rows, row)
	}
	for _, a := range order {
		year.Accounts = append(year.Accounts, short[a])
	}
	var grand float64
	for _, v := range accSum {
		year.Foot = append(year.Foot, moneyOrDash(v))
		grand += v
	}
	year.Foot = append(year.Foot, moneyOrDash(grand))

	if len(t.Months) == 1 {
		var sumAdv, sumInv, sumTotal float64
		for _, ln := range t.Months[0].Lines {
			var adv, inv float64
			if ln.Advance != nil {
				adv = ln.Advance.sum()
			}
			if ln.Invoice != nil {
				inv = ln.Invoice.sum()
			}
			sumAdv += adv
			sumInv += inv
			sumTotal += adv + inv
			res = append(res, moneyResRow{
				Label:   ln.Short,
				Advance: moneyOrDash(adv),
				Invoice: moneyOrDash(inv),
				Total:   ln.TotalText,
			})
		}
		total.Advance = moneyOrDash(sumAdv)
		total.Invoice = moneyOrDash(sumInv)
		total.Total = moneyOrDash(sumTotal)
	}
	return
}

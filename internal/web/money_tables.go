package web

import (
	"strconv"
	"time"

	"indextro/internal/model"
)

// scopeAccounts — какие лицевые счета входят в выбранный фильтр «Что включить».
func scopeAccounts(scope string) []model.Account {
	switch scope {
	case "water":
		return []model.Account{model.AccountWaterCold, model.AccountWaterHot}
	case "energy":
		return []model.Account{model.AccountEnergy}
	}
	return []model.Account{model.AccountWaterCold, model.AccountWaterHot, model.AccountEnergy}
}

// scopeLabel — подпись набора счетов для таблицы начислений.
func scopeLabel(scope string) string {
	switch scope {
	case "water":
		return "ХВС и ГВС"
	case "energy":
		return "Электроэнергия"
	}
	return "все счета"
}

// buildMoneyRange подготавливает таблицу начислений за период: строка на
// каждый месяц, суммы — по счетам, выбранным фильтром.
func (s *Server) buildMoneyRange(obj model.Object, from, to model.Period,
	today model.Period, scope string, editable bool) moneyYearData {

	stored, _ := s.Store.MoneyForAccountsIn(obj.ID, scopeAccounts(scope), from, to)

	data := moneyYearData{
		Object:      obj,
		Year:        to.Year,
		YearLabel:   strconv.Itoa(to.Year) + " год",
		AccountLbl:  scopeLabel(scope),
		Editable:    editable,
		From:        from,
		To:          to,
		FromLabel:   from.Label(),
		ToLabel:     to.Label(),
		RangeLabel:  rangeText(from, to),
		SingleMonth: from == to,
		MonthLabel:  from.MonthName(),
	}

	var advTotal, invTotal, advPaid, invPaid float64
	for p := from; !p.After(to); p = p.Next() {
		rec := stored[p.String()]
		rec.Period = p.String()
		row := moneyTableRowFrom(model.AccountWaterCold, p, rec, p == today)
		// В журнале таблица только показывает: суммы правятся на главной,
		// где каждый лицевой счёт выведен отдельно.
		if !editable {
			row.Advance.Locked = true
			row.Invoice.Locked = true
			// При фильтре «Вода» или «Электро» набор счетов однозначен —
			// отметку оплаты можно ставить прямо здесь. При «Всё» непонятно,
			// какие счета оплачены, поэтому там только просмотр.
			if scope != "all" {
				row.Advance.Toggleable = true
				row.Invoice.Toggleable = true
			}
		}
		data.Rows = append(data.Rows, row)
		if row.HasAny {
			data.HasAny = true
		}
		// Итоги считаем по позициям: месяц может быть оплачен частично,
		// и «Оплачено» должно учитывать только оплаченное.
		advTotal += rec.AdvanceSum
		invTotal += rec.InvoiceSum
		if rec.AdvancePaid {
			advPaid += rec.AdvanceSum
		}
		if rec.InvoicePaid {
			invPaid += rec.InvoiceSum
		}
	}
	data.Totals = moneyTotalAggregate(advTotal, invTotal, advPaid, invPaid)
	return data
}

// moneyTotalAggregate собирает итог таблицы из выставленного и оплаченного.
func moneyTotalAggregate(advTotal, invTotal, advPaid, invPaid float64) moneyTableTotalView {
	advTotal = model.Round2(advTotal)
	invTotal = model.Round2(invTotal)
	advPaid = model.Round2(advPaid)
	invPaid = model.Round2(invPaid)
	paid := model.Round2(advPaid + invPaid)
	total := model.Round2(advTotal + invTotal)
	due := model.Round2(total - paid)

	adv := moneyCellFrom(model.Period{}, "advance_sum", advTotal, advTotal > 0 && advPaid >= advTotal)
	inv := moneyCellFrom(model.Period{}, "invoice_sum", invTotal, invTotal > 0 && invPaid >= invTotal)
	return moneyTableTotalView{
		Advance:   adv,
		Invoice:   inv,
		Total:     total,
		TotalText: moneyOrDash(total),
		Paid:      paid,
		PaidText:  model.FormatMoney(paid),
		Due:       due,
		DueText:   model.FormatMoney(due),
		HasAny:    adv.HasSum || inv.HasSum,
	}
}

// ---------------------------------------------------------------- начисления месяца

// financeRowView — строка блока «Начисления» на главной: один лицевой счёт
// за выбранный месяц.
type financeRowView struct {
	Account     string
	Label       string
	AdvanceText string
	AdvancePaid bool
	ShowAdvance bool
	ChargedText string
	Paid        bool
	ShowBill    bool
	TotalText   string
	Status      string
	StatusRu    string
}

// financeRowFrom переводит строку финансов в строку таблицы.
func financeRowFrom(row moneyRowView) financeRowView {
	status, statusRu := "empty", "Не задан"
	switch {
	case row.Paid:
		status, statusRu = "paid", "Оплачен"
	case row.ChargedRaw != 0 || row.AdvanceRaw != 0:
		status, statusRu = "due", "Не оплачен"
	}
	total := model.Round2(row.ChargedRaw + row.AdvanceRaw)
	totalText := "—"
	if total != 0 {
		totalText = model.FormatMoney(total)
	}
	return financeRowView{
		Account:     row.Account,
		Label:       row.Label,
		AdvanceText: row.AdvanceText,
		AdvancePaid: row.AdvancePaid,
		ShowAdvance: row.ShowAdvance,
		ChargedText: row.ChargedText,
		Paid:        row.Paid,
		ShowBill:    row.ShowBill,
		TotalText:   totalText,
		Status:      status,
		StatusRu:    statusRu,
	}
}

// ---------------------------------------------------------------- таблица «Итоги»

// journalTableData — таблица «Итоги» в том же рисунке, что у начислений:
// месяц слева, значения по ресурсам, справа кнопка документа.
type journalTableData struct {
	Cols          []journalCol
	Rows          []journalTableRow
	Foot          []string
	ShowFoot      bool
	Months        int
	MonthsText    string
	StatementHref string
}

// journalCol — колонка «Итогов»: ресурс и что в ней — показание или расход.
type journalCol struct {
	Title   string
	IsLast  bool // последняя колонка ресурса: после неё разделитель
}

// journalTableRow — строка «Итогов»: подпись и значения по колонкам.
type journalTableRow struct {
	Label string
	Href  string
	Current bool
	Empty bool
	Cells []string
}

// buildJournalTable раскладывает журнал показаний по той же сетке, что
// таблица начислений: месяц, по ресурсу «показание» и «расход», итог.
func buildJournalTable(rows []journalRowView, resources []model.Resource,
	totals []journalCellView, monthsCount int, rangeLabel, statementHref string) journalTableData {

	td := journalTableData{
		Months:        monthsCount,
		MonthsText:    model.PluralRu(monthsCount, "месяц", "месяца", "месяцев"),
		StatementHref: statementHref,
	}
	for _, r := range resources {
		td.Cols = append(td.Cols, journalCol{Title: r.Short() + " · показание"})
		td.Cols = append(td.Cols, journalCol{Title: r.Short() + " · расход", IsLast: true})
	}
	for _, row := range rows {
		jt := journalTableRow{Label: row.Label, Href: row.Href, Current: row.IsCurrent, Empty: !row.HasAny}
		for _, c := range row.Cells {
			jt.Cells = append(jt.Cells, pickValue(c.Reading), pickValue(c.Consumption))
		}
		td.Rows = append(td.Rows, jt)
	}
	if len(totals) > 0 {
		td.ShowFoot = true
		td.Foot = append(td.Foot, "Итого")
		for _, c := range totals {
			td.Foot = append(td.Foot, "—")
			if c.Consumption != "" {
				td.Foot = append(td.Foot, c.Consumption+" "+c.Unit)
			} else {
				td.Foot = append(td.Foot, "—")
			}
		}
	}
	_ = rangeLabel
	return td
}

func pickValue(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// journalTotal — плашка сводки над таблицей «Итоги».
type journalTotal struct {
	Label string
	Value string
	Unit  string
}

var _ = time.Now

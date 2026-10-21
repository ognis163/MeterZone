package web

import (
	"indextro/internal/model"
)

// Таблица начислений в журнале: на каждый лицевой счёт своя колонка в
// группах «Аванс» и «Счёт». Всё редактируется прямо здесь: сумма и отметка
// оплаты по каждому счёту отдельно.

// accountTone — цвет колонки счёта: холодная вода, горячая, электричество.
func accountTone(acc model.Account) string {
	switch acc {
	case model.AccountWaterCold, model.AccountWater:
		return "cold"
	case model.AccountWaterHot:
		return "hot"
	}
	return "energy"
}

// moneyGridCol — колонка счёта в таблице начислений.
type moneyGridCol struct {
	Account string
	Short   string
	Label   string
	Tone    string
}

// moneyGridCell — сумма и отметка оплаты одного счёта за месяц.
type moneyGridCell struct {
	Period   string
	Account  string
	Field    string // advance | charged
	Tone     string
	Value    string
	Paid     bool
	HasSum   bool
	Unpaid   bool // выставлено, но не оплачено — такую ячейку подсвечиваем
	Status   string
	StatusRu string
	Locked   bool
}

// moneyGridRow — строка месяца.
type moneyGridRow struct {
	Label     string
	Period    string
	Current   bool
	Advance   []moneyGridCell
	Invoice   []moneyGridCell
	Total     float64
	TotalText string
	Unpaid    bool // в месяце есть неоплаченное
}

// moneyGrid — вся таблица начислений журнала.
type moneyGrid struct {
	Object        model.Object
	RangeLabel    string
	AdvCols       []moneyGridCol
	InvCols       []moneyGridCol
	Rows          []moneyGridRow
	Accounts      []moneyGridCol // счета в порядке показа
	Cards         []moneyCard
	AdvTotals     []string
	InvTotals     []string
	Total         float64
	TotalText     string
	Paid          float64
	PaidText      string
	Due           float64
	DueText       string
	HasAny        bool
	Editable      bool
	StatementHref string
}

// moneyCard — месяц в виде карточки: три счёта, аванс и счёт в каждой строке.
type moneyCard struct {
	Label     string
	Period    string
	Current   bool
	TotalText string
	Lines     []moneyCardLine
}

// mergeAccountCols объединяет списки счетов аванса и счёта без повторов.
func mergeAccountCols(lists ...[]moneyGridCol) []moneyGridCol {
	seen := map[string]bool{}
	var out []moneyGridCol
	for _, list := range lists {
		for _, c := range list {
			if seen[c.Account] {
				continue
			}
			seen[c.Account] = true
			out = append(out, c)
		}
	}
	return out
}
// moneyCardLine — строка счёта в карточке месяца.
type moneyCardLine struct {
	Short   string
	Tone    string
	Advance *moneyGridCell
	Invoice *moneyGridCell
}
// moneyGridCellFrom собирает ячейку счёта за месяц.
func moneyGridCellFrom(col moneyGridCol, p model.Period, field string, rec model.Record, paid bool, sum float64) moneyGridCell {
	cell := moneyGridCell{
		Period:  p.String(),
		Account: col.Account,
		Field:   field,
		Tone:    col.Tone,
		Value:   formatMoneyInput(sum),
		Paid:    paid && sum != 0,
		HasSum:  sum != 0,
	}
	cell.Status, cell.StatusRu = "empty", "Не задан"
	if cell.HasSum {
		if cell.Paid {
			cell.Status, cell.StatusRu = "paid", "Оплачен"
		} else {
			cell.Status, cell.StatusRu = "due", "Не оплачен"
			cell.Unpaid = true
		}
	}
	return cell
}

// buildMoneyGrid собирает таблицу начислений за период по выбранным счетам.
func (s *Server) buildMoneyGrid(obj model.Object, from, to model.Period,
	today model.Period, scope string, editable bool, statementHref string) moneyGrid {

	advAccounts, invAccounts := s.financeAccounts(obj.ID, scope)
	stored, _ := s.Store.MoneyGridIn(obj.ID, from, to)

	grid := moneyGrid{
		Object:        obj,
		RangeLabel:    rangeText(from, to),
		Editable:      editable,
		StatementHref: statementHref,
	}
	for _, acc := range advAccounts {
		grid.AdvCols = append(grid.AdvCols, moneyGridCol{
			Account: string(acc), Short: acc.Short(), Label: acc.Label(), Tone: accountTone(acc),
		})
	}
	for _, acc := range invAccounts {
		grid.InvCols = append(grid.InvCols, moneyGridCol{
			Account: string(acc), Short: acc.Short(), Label: acc.Label(), Tone: accountTone(acc),
		})
	}

	for p := from; !p.After(to); p = p.Next() {
		month := stored[p.String()]
		row := moneyGridRow{
			Label:   p.Label(),
			Period:  p.String(),
			Current: p == today,
		}
		for _, col := range grid.AdvCols {
			acc := model.Account(col.Account)
			rec := month[acc]
			cell := moneyGridCellFrom(col, p, "advance", rec, rec.AdvancePaid, rec.AdvanceSum)
			row.Advance = append(row.Advance, cell)
			row.Total += rec.AdvanceSum
			if cell.Unpaid {
				row.Unpaid = true
			}
			if cell.HasSum {
				grid.HasAny = true
			}
		}
		for _, col := range grid.InvCols {
			acc := model.Account(col.Account)
			rec := month[acc]
			cell := moneyGridCellFrom(col, p, "charged", rec, rec.InvoicePaid, rec.InvoiceSum)
			row.Invoice = append(row.Invoice, cell)
			row.Total += rec.InvoiceSum
			if cell.Unpaid {
				row.Unpaid = true
			}
			if cell.HasSum {
				grid.HasAny = true
			}
		}
		row.Total = model.Round2(row.Total)
		if row.Total != 0 {
			row.TotalText = model.FormatMoney(row.Total)
		} else {
			row.TotalText = "—"
		}
		grid.Rows = append(grid.Rows, row)
	}

	// Сводка: оплаченное и остаток по тем же счетам и месяцам.
	for _, row := range grid.Rows {
		for _, cell := range row.Advance {
			if cell.Paid {
				grid.Paid += cell.sum()
			} else {
				grid.Due += cell.sum()
			}
		}
		for _, cell := range row.Invoice {
			if cell.Paid {
				grid.Paid += cell.sum()
			} else {
				grid.Due += cell.sum()
			}
		}
	}
	// Итоги по каждой колонке — в подвале таблицы.
	advSums := make([]float64, len(grid.AdvCols))
	invSums := make([]float64, len(grid.InvCols))
	for _, row := range grid.Rows {
		for i, cell := range row.Advance {
			advSums[i] += cell.sum()
		}
		for i, cell := range row.Invoice {
			invSums[i] += cell.sum()
		}
	}
	for _, v := range advSums {
		grid.AdvTotals = append(grid.AdvTotals, moneyOrDash(v))
	}
	for _, v := range invSums {
		grid.InvTotals = append(grid.InvTotals, moneyOrDash(v))
	}

	grid.Paid = model.Round2(grid.Paid)
	grid.Due = model.Round2(grid.Due)
	for _, row := range grid.Rows {
		grid.Total += row.Total
	}
	grid.Total = model.Round2(grid.Total)
	grid.TotalText = moneyOrDash(grid.Total)
	// Карточки месяцев: строки по счетам, в каждой — аванс и счёт.
	grid.Accounts = mergeAccountCols(grid.AdvCols, grid.InvCols)
	for _, row := range grid.Rows {
		card := moneyCard{
			Label:     row.Label,
			Period:    row.Period,
			Current:   row.Current,
			TotalText: row.TotalText,
		}
		adv := map[string]moneyGridCell{}
		for _, c := range row.Advance {
			adv[c.Account] = c
		}
		inv := map[string]moneyGridCell{}
		for _, c := range row.Invoice {
			inv[c.Account] = c
		}
		for _, acc := range grid.Accounts {
			line := moneyCardLine{Short: acc.Short, Tone: acc.Tone}
			if c, ok := adv[acc.Account]; ok {
				cell := c
				line.Advance = &cell
			}
			if c, ok := inv[acc.Account]; ok {
				cell := c
				line.Invoice = &cell
			}
			card.Lines = append(card.Lines, line)
		}
		grid.Cards = append(grid.Cards, card)
	}
	grid.PaidText = model.FormatMoney(grid.Paid)
	grid.DueText = model.FormatMoney(grid.Due)
	return grid
}

// sum — сумма ячейки из строки ввода.
func (c moneyGridCell) sum() float64 {
	if c.Value == "" {
		return 0
	}
	v, err := model.ParseValue(c.Value)
	if err != nil {
		return 0
	}
	return model.Round2(v)
}

// financeAccounts определяет, по каким счетам вести аванс и счёт. Набор
// берётся из фильтра, а внутри набора — из настроек приборов: если у счётчика
// включён только счёт, аванса по этому ресурсу не будет.
func (s *Server) financeAccounts(objectID int64, scope string) (advance, invoice []model.Account) {
	accounts := scopeAccounts(scope)
	meters, _ := s.Store.Meters(objectID, true)

	advOn := map[model.Account]bool{}
	invOn := map[model.Account]bool{}
	configured := false
	for _, m := range meters {
		mode := m.FinanceModeValue()
		if mode == model.FinanceNone {
			continue
		}
		configured = true
		acc := model.AccountFor(m.Resource)
		if m.ShowAdvance() {
			advOn[acc] = true
		}
		if m.ShowBill() {
			invOn[acc] = true
		}
	}

	for _, acc := range accounts {
		// Если финучёт вообще не настроен, показываем оба столбца — чтобы
		// можно было внести суммы прямо в журнале.
		if !configured || advOn[acc] {
			advance = append(advance, acc)
		}
		if !configured || invOn[acc] {
			invoice = append(invoice, acc)
		}
	}
	return advance, invoice
}

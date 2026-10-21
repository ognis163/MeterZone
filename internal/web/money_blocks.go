package web

import (
	"indextro/internal/model"
)

// Начисления в журнале — одна таблица на все ресурсы: месяц, ресурс,
// аванс, счёт и итого. Аванс и счёт можно отметить оплаченными по
// отдельности, а «Итого» отмечает сразу обе позиции строки.

// moneyGridCell — позиция строки: сумма и отметка оплаты одного счёта за месяц.
type moneyGridCell struct {
	Period   string
	Account  string
	Field    string // advance | charged
	Value    string
	Paid     bool
	HasSum   bool
	Unpaid   bool
	Status   string
	StatusRu string
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

// moneyTableView — таблица начислений за выбранный период.
type moneyTableView struct {
	Months      []moneyMonthView
	ShowAdvance bool // есть хотя бы один счёт с авансом
	ShowInvoice bool // есть хотя бы один счёт со счётом
	Enabled     bool // финучёт включён хотя бы у одного прибора
}

// moneyMonthView — месяц: подпись слева и строка на каждый ресурс.
type moneyMonthView struct {
	Label   string
	Period  string
	Current bool
	Lines   []moneyLineView
}

// moneyLineView — строка счёта за месяц.
type moneyLineView struct {
	Label       string // «Холодная вода»
	Short       string // «ХВС» — для узких экранов
	Account     string
	ShowAdvance bool
	ShowInvoice bool
	Advance     *moneyGridCell
	Invoice     *moneyGridCell
	Total       float64
	TotalText   string
	HasDue      bool
	Due         float64
	DueText     string
	Paid        bool // оплачены все позиции строки
}

// buildMoneyTable собирает таблицу начислений за период.
func (s *Server) buildMoneyTable(obj model.Object, from, to model.Period,
	today model.Period, scope string) (table moneyTableView, paid, due float64) {

	stored, _ := s.Store.MoneyGridIn(obj.ID, from, to)
	meters, _ := s.Store.Meters(obj.ID, true)

	advOn := map[model.Account]bool{}
	invOn := map[model.Account]bool{}
	on := map[model.Account]bool{}
	for _, m := range meters {
		if m.FinanceModeValue() == model.FinanceNone {
			continue
		}
		acc := model.AccountFor(m.Resource)
		on[acc] = true
		if m.ShowAdvance() {
			advOn[acc] = true
		}
		if m.ShowBill() {
			invOn[acc] = true
		}
	}

	accounts := make([]model.Account, 0, len(model.AccountOrder))
	for _, acc := range model.AccountOrder {
		if !on[acc] {
			continue
		}
		table.Enabled = true
		if !moneyAccountInScope(acc, scope) {
			continue
		}
		accounts = append(accounts, acc)
		table.ShowAdvance = table.ShowAdvance || advOn[acc]
		table.ShowInvoice = table.ShowInvoice || invOn[acc]
	}
	if len(accounts) == 0 {
		return table, 0, 0
	}

	for p := from; !p.After(to); p = p.Next() {
		month := moneyMonthView{
			Label:   p.Label(),
			Period:  p.String(),
			Current: p == today,
		}
		for _, acc := range accounts {
			rec := stored[p.String()][acc]
			line := moneyLineView{
				Label:       acc.Label(),
				Short:       acc.Short(),
				Account:     string(acc),
				ShowAdvance: advOn[acc],
				ShowInvoice: invOn[acc],
			}
			if line.ShowAdvance {
				cell := moneyCellFor(acc, p, "advance", rec.AdvanceSum, rec.AdvancePaid)
				line.Advance = &cell
				line.Total += cell.sum()
				if !cell.Paid && cell.HasSum {
					line.Due += cell.sum()
				}
			}
			if line.ShowInvoice {
				cell := moneyCellFor(acc, p, "charged", rec.InvoiceSum, rec.InvoicePaid)
				line.Invoice = &cell
				line.Total += cell.sum()
				if !cell.Paid && cell.HasSum {
					line.Due += cell.sum()
				}
			}
			line.Total = model.Round2(line.Total)
			line.Due = model.Round2(line.Due)
			line.TotalText = moneyOrDash(line.Total)
			line.DueText = model.FormatMoney(line.Due)
			line.HasDue = line.Due > 0
			line.Paid = line.Total > 0 && line.Due == 0
			paid += line.Total - line.Due
			due += line.Due
			month.Lines = append(month.Lines, line)
		}
		table.Months = append(table.Months, month)
	}
	return table, model.Round2(paid), model.Round2(due)
}

// moneyAccountInScope — входит ли счёт в фильтр «Что включить».
func moneyAccountInScope(acc model.Account, scope string) bool {
	switch scope {
	case "water":
		return acc == model.AccountWaterCold || acc == model.AccountWaterHot
	case "energy":
		return acc == model.AccountEnergy
	}
	return true
}

// moneyCellFor собирает ячейку позиции: сумму, оплату и оформление.
func moneyCellFor(acc model.Account, p model.Period, field string, sum float64, paid bool) moneyGridCell {
	cell := moneyGridCell{
		Period:  p.String(),
		Account: string(acc),
		Field:   field,
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

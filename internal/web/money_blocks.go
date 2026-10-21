package web

import (
	"indextro/internal/model"
)

// Начисления в журнале разбиты на два блока, как плитки показаний на главной:
// «Электроэнергия» и «Водоснабжение». Внутри — месяц, аванс, счёт и итого.
// Блок появляется, только если у прибора включён финансовый учёт.

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
// moneyBlockView — блок начислений одного вида: электро или вода.
type moneyBlockView struct {
	Title       string
	IsWater     bool
	ShowAdvance bool
	ShowInvoice bool
	Months      []moneyMonthView
	HasAny      bool
}

// moneyMonthView — месяц в блоке; у воды в нём две строки: холодная и горячая.
type moneyMonthView struct {
	Label   string
	Period  string
	Current bool
	Lines   []moneyLineView
}

// moneyLineView — строка счёта за месяц.
type moneyLineView struct {
	Label     string // «Холодная» / «Горячая»; у электро пусто
	Account   string
	Advance   *moneyGridCell
	Invoice   *moneyGridCell
	Total     float64
	TotalText string
	HasDue    bool
	Due       float64
	DueText   string
	Paid      bool // оплачены все позиции строки
}

// buildMoneyBlocks собирает блоки начислений за период.
func (s *Server) buildMoneyBlocks(obj model.Object, from, to model.Period,
	today model.Period, scope string) (blocks []moneyBlockView, paid, due float64) {

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

	// Электроэнергия идёт первой, вода — ниже.
	type blockSpec struct {
		title   string
		isWater bool
		account model.Account
		line    string
	}
	specs := []blockSpec{}
	if scope != "water" {
		specs = append(specs, blockSpec{"Электроэнергия", false, model.AccountEnergy, ""})
	}
	if scope != "energy" {
		specs = append(specs, blockSpec{"Водоснабжение", true, model.AccountWaterCold, "Холодная"})
		specs = append(specs, blockSpec{"Водоснабжение", true, model.AccountWaterHot, "Горячая"})
	}

	grouped := map[bool]*moneyBlockView{}
	for _, sp := range specs {
		if !on[sp.account] {
			continue // в приборе не включён финансовый учёт
		}
		blk := grouped[sp.isWater]
		if blk == nil {
			blk = &moneyBlockView{Title: sp.title, IsWater: sp.isWater}
			grouped[sp.isWater] = blk
		}
		if advOn[sp.account] {
			blk.ShowAdvance = true
		}
		if invOn[sp.account] {
			blk.ShowInvoice = true
		}
	}

	if e := grouped[false]; e != nil {
		blocks = append(blocks, *e)
	}
	if w := grouped[true]; w != nil {
		blocks = append(blocks, *w)
	}

	// Месяцы в каждом блоке.
	for bi := range blocks {
		blk := &blocks[bi]
		accounts := []model.Account{model.AccountEnergy}
		if blk.IsWater {
			accounts = []model.Account{model.AccountWaterCold, model.AccountWaterHot}
		}
		for p := from; !p.After(to); p = p.Next() {
			month := moneyMonthView{Label: p.Label(), Period: p.String(), Current: p == today}
			for _, acc := range accounts {
				if !on[acc] {
					continue
				}
				rec := stored[p.String()][acc]
				line := moneyLineView{Account: string(acc), Label: accountLineLabel(acc)}
				if blk.ShowAdvance && advOn[acc] {
					cell := moneyCellFor(acc, p, "advance", rec.AdvanceSum, rec.AdvancePaid)
					line.Advance = &cell
					line.Total += cell.sum()
					if !cell.Paid && cell.HasSum {
						line.Due += cell.sum()
					}
				}
				if blk.ShowInvoice && invOn[acc] {
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
				if line.Total > 0 {
					blk.HasAny = true
				}
				paid += line.Total - line.Due
				due += line.Due
				month.Lines = append(month.Lines, line)
			}
			blk.Months = append(blk.Months, month)
		}
	}
	return blocks, model.Round2(paid), model.Round2(due)
}

// accountLineLabel — подпись строки счёта в блоке воды.
func accountLineLabel(acc model.Account) string {
	switch acc {
	case model.AccountWaterCold:
		return "Холодная"
	case model.AccountWaterHot:
		return "Горячая"
	}
	return ""
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

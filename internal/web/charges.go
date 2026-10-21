package web

import (
	"strconv"
	"time"

	"indextro/internal/docgen"
	"indextro/internal/model"
)

// Таблица «Авансы и счета»: 12 месяцев года, по каждой позиции — сумма
// и отметка об оплате. Статус позиции вычисляется, как в эталоне:
// оплачена — «Оплачен», пустая — «Не задан», иначе «Не оплачен».
// На странице «Начисления» позиции редактируются, на «Журнале» и
// в ведомости — только читаются.

// moneyCellView — позиция месяца: аванс или счёт.
type moneyCellView struct {
	Period   string // YYYY-M: ключ записи
	Field    string // advance_sum | invoice_sum
	SumRaw   float64
	SumText  string // «12 000» — для поля ввода (без разрядов не нужен, ёмкость поля ограничена)
	SumValue string // «12000» или «12000,5» — для value
	Paid     bool
	HasSum   bool
	Status   string // paid | due | empty
	StatusRu string
}

// moneyTableRowView — строка месяца в таблице начислений.
type moneyTableRowView struct {
	Period    string
	PeriodRaw model.Period
	Label     string
	IsCurrent bool
	Advance   moneyCellView
	Invoice   moneyCellView
	Total     float64
	TotalText string // «—», если в строке пусто
	HasAny    bool
}

// moneyTableTotalView — итог под таблицей и сводка над ней.
type moneyTableTotalView struct {
	Advance     moneyCellView
	Invoice     moneyCellView
	Total       float64
	TotalText   string
	Paid        float64
	PaidText    string
	Due         float64
	DueText     string
	HasAny      bool
	CountPaid   int
	CountDue    int
	CountEmpty int
}

// moneyCellFrom собирает позицию для отображения и ввода.
func moneyCellFrom(p model.Period, field string, sum float64, paid bool) moneyCellView {
	sum = model.Round2(sum)
	c := moneyCellView{
		Period:  p.String(),
		Field:   field,
		SumRaw:  sum,
		SumText: model.FormatValue(sum),
		HasSum:  sum != 0,
		Paid:    paid,
	}
	if sum != 0 {
		c.SumValue = model.FormatInput(sum)
	}
	switch {
	case c.Paid:
		c.Status, c.StatusRu = "paid", "Оплачен"
	case !c.HasSum:
		c.Status, c.StatusRu = "empty", "Не задан"
	default:
		c.Status, c.StatusRu = "due", "Не оплачен"
	}
	return c
}

// moneyTableRowFrom — строка таблицы по записи месяца.
func moneyTableRowFrom(_ model.Account, p model.Period, rec model.Record, current bool) moneyTableRowView {
	row := moneyTableRowView{
		Period:    p.String(),
		PeriodRaw: p,
		Label:     p.MonthName(),
		IsCurrent: current,
		Advance:   moneyCellFrom(p, "advance_sum", rec.AdvanceSum, rec.AdvancePaid),
		Invoice:   moneyCellFrom(p, "invoice_sum", rec.InvoiceSum, rec.InvoicePaid),
		Total:     rec.Total(),
	}
	row.TotalText = moneyOrDash(row.Total)
	row.HasAny = row.Advance.HasSum || row.Invoice.HasSum
	return row
}

// moneyTotalFrom сводит итог по записям года.
func moneyTotalFrom(t model.Record) moneyTableTotalView {
	adv := moneyCellFrom(model.Period{}, "advance_sum", t.AdvanceSum, t.AdvancePaid)
	inv := moneyCellFrom(model.Period{}, "invoice_sum", t.InvoiceSum, t.InvoicePaid)
	return moneyTableTotalView{
		Advance:   adv,
		Invoice:   inv,
		Total:     t.Total(),
		TotalText: moneyOrDash(t.Total()),
		Paid:      t.Paid(),
		PaidText:  model.FormatMoney(t.Paid()),
		Due:       t.Due(),
		DueText:   model.FormatMoney(t.Due()),
		HasAny:    adv.HasSum || inv.HasSum,
	}
}

// moneyOrDash прячет ноль под прочерком.
func moneyOrDash(v float64) string {
	if model.Round2(v) == 0 {
		return "—"
	}
	return model.FormatMoney(v)
}

// ---------------------------------------------------------------- данные года

// moneyYearData — всё, что нужно таблице за год: строки и итог.
// periodKey — месяц, который открыт в вводе показаний (журнал) или null.
type moneyYearData struct {
	Object        model.Object
	Year          int
	YearLabel     string
	PrevHref      string // соседние годы
	NextHref      string
	SelfHref      string // ссылка на этот же год
	JournalHref   string // назад в журнал
	StatementHref string // куда ведёт «Печать\отправить»

	Account       string
	AccountLbl    string
	Rows          []moneyTableRowView
	Totals        moneyTableTotalView
	HasAny        bool
	Editable      bool

	// Период таблицы: на странице начислений это весь год, в журнале —
	// выбранный диапазон.
	From        model.Period
	To          model.Period
	FromLabel   string
	ToLabel     string
	RangeLabel  string
	SingleMonth bool
	MonthLabel  string // подпись одной строки, если период — один месяц
}

// buildMoneyYear подготавливает строки таблицы за год.
func (s *Server) buildMoneyYear(obj model.Object, year int, editable bool) moneyYearData {
	now := time.Now().In(s.Loc)
	current := model.PeriodOf(now)
	account := s.journalMoneyAccount(obj.ID, model.Period{Year: year, Month: 1})
	stored, _ := s.Store.MoneyAccountForYear(obj.ID, account, year)

	data := moneyYearData{
		Object:     obj,
		Year:       year,
		YearLabel:  strconv.Itoa(year) + " год",
		Account:    string(account),
		AccountLbl: account.Label(),
		Editable:   editable,
	}
	var total model.Record
	for m := 1; m <= 12; m++ {
		p, err := model.NewPeriod(year, m)
		if err != nil {
			continue
		}
		rec := stored[p.String()]
		rec.Period = p.String()
		row := moneyTableRowFrom(account, p, rec, current.Year == year && current.Month == m)
		data.Rows = append(data.Rows, row)
		total.AdvanceSum += rec.AdvanceSum
		total.AdvancePaid = total.AdvancePaid || rec.AdvancePaid
		total.InvoiceSum += rec.InvoiceSum
		total.InvoicePaid = total.InvoicePaid || rec.InvoicePaid
		if row.HasAny {
			data.HasAny = true
		}
	}
	total.AdvanceSum = model.Round2(total.AdvanceSum)
	total.InvoiceSum = model.Round2(total.InvoiceSum)
	data.Totals = moneyTotalFrom(total)
	return data
}

// ---------------------------------------------------------------- начисления месяца

// financeRowView — строка блока «Начисления» на главной: один лицевой счёт
// за выбранный месяц в том же рисунке, что таблица журнала.
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
// месяц слева, значения по ресурсам, справа итог и кнопка документа.
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
	Title   string // показание / расход
	Resource string
	IsLast  bool // последняя колонка ресурса: после неё разделитель
}

type journalTableRow struct {
	Label    string
	Href     string
	Current  bool
	Empty    bool
	Cells    []string
	Total    string
}

// buildJournalTable раскладывает журнал показаний по той же сетке, что
// таблица начислений: месяц, по ресурсу «показание» и «расход», итог.
func buildJournalTable(rows []journalRowView, resources []model.Resource,
	totals []journalCellView, monthsCount int, rangeLabel, statementHref string) journalTableData {

	td := journalTableData{Months: monthsCount, MonthsText: model.PluralRu(monthsCount, "месяц", "месяца", "месяцев"), StatementHref: statementHref}
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
	return td
}

// journalTotal — плашка сводки над таблицей «Итоги». 
type journalTotal struct {
	Label string
	Value string
	Unit  string
}

func pickValue(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// buildMoneyRange подготавливает таблицу начислений за произвольный период:
// строка на каждый месяц. В журнале суммы и отметки можно менять прямо
// в таблице, на странице начислений — тоже.
func (s *Server) buildMoneyRange(obj model.Object, from, to model.Period, today model.Period, editable bool) moneyYearData {
	account := s.journalMoneyAccount(obj.ID, from)
	stored, _ := s.Store.MoneyAccountIn(obj.ID, account, from, to)

	data := moneyYearData{
		Object:      obj,
		Year:        to.Year,
		YearLabel:   strconv.Itoa(to.Year) + " год",
		Account:     string(account),
		AccountLbl:  account.Label(),
		Editable:    editable,
		From:        from,
		To:          to,
		FromLabel:   from.Label(),
		ToLabel:     to.Label(),
		RangeLabel:  rangeText(from, to),
		SingleMonth: from == to,
		MonthLabel:  from.MonthName(),
	}
	var total model.Record
	for p := from; !p.After(to); p = p.Next() {
		rec := stored[p.String()]
		rec.Period = p.String()
		row := moneyTableRowFrom(account, p, rec, p == today)
		data.Rows = append(data.Rows, row)
		total.AdvanceSum += rec.AdvanceSum
		total.AdvancePaid = total.AdvancePaid || rec.AdvancePaid
		total.InvoiceSum += rec.InvoiceSum
		total.InvoicePaid = total.InvoicePaid || rec.InvoicePaid
		if row.HasAny {
			data.HasAny = true
		}
	}
	total.AdvanceSum = model.Round2(total.AdvanceSum)
	total.InvoiceSum = model.Round2(total.InvoiceSum)
	data.Totals = moneyTotalFrom(total)
	return data
}

// rangeText — подпись периода для заголовка таблицы.
func rangeText(from, to model.Period) string {
	switch {
	case from == to:
		return from.Accusative() + " г."
	case from.Year == to.Year && from.Month == 1 && to.Month == 12:
		return strconv.Itoa(from.Year) + " год"
	case from.Year == to.Year:
		return from.MonthName() + " — " + to.MonthName() + " " + strconv.Itoa(to.Year)
	}
	return from.Label() + " — " + to.Label()
}

// ---------------------------------------------------------------- документ

// chargesDocument собирает ведомость начислений для экрана и PDF.
func chargesDocument(year moneyYearData) docgen.Document {
	object := year.Object
	subtitle := "за " + strconv.Itoa(year.Year) + " год"
	if year.RangeLabel != "" {
		subtitle = "за " + year.RangeLabel
	}
	d := docgen.Document{
		Kind:      "charges",
		Title:     "Ведомость начислений",
		Subtitle:  subtitle,
		Generated: time.Now(),
		// Название объекта — из карточки объекта, между собственником и адресом.
		Meta: []docgen.Meta{
			{Label: "Собственник", Lines: []string{dashText(object.Owner)}},
			{Label: "Имя объекта", Lines: []string{dashText(object.Title())}},
		},
	}
	if object.Address != "" {
		d.Meta = append(d.Meta, docgen.Meta{Label: "Адрес объекта", Lines: []string{object.Address}})
	}
	// Заголовок раздела пустой: черты под названием нет. Строки «Итого»
	// здесь тоже нет — итоги идут отдельной таблицей ниже.
	sec := docgen.Section{
		Title: "",
		Columns: []docgen.Column{
			docgen.Column{Title: "Месяц", Width: 34, Align: docgen.Left},
			docgen.Column{Title: "Аванс", Width: 30, Align: docgen.Right},
			docgen.Column{Title: "Счёт", Width: 30, Align: docgen.Right},
			docgen.Column{Title: "Итого", Width: 30, Align: docgen.Right},
			docgen.Column{Title: "Оплата", Width: 40, Align: docgen.Right},
		},
	}
	for _, row := range year.Rows {
		paid := paidState(row.Advance) && paidState(row.Invoice)
		hasAny := row.Advance.HasSum || row.Invoice.HasSum
		sec.Rows = append(sec.Rows, docgen.Row{
			// Неоплаченный месяц выделяем серым.
			Shade: hasAny && !paid,
			Cells: []docgen.Cell{
				{Text: row.Label},
				// Подчёркиваем именно неоплаченное: аванс или счёт.
				moneyDocSumMarked(row.Advance),
				moneyDocSumMarked(row.Invoice),
				{Text: row.TotalText, Muted: row.TotalText == "—"},
				moneyDocPaid(row.Advance, row.Invoice),
			},
		})
	}
	d.Sections = append(d.Sections, sec)

	// Итоги: сколько выставлено, оплачено и осталось. По каждому
	// параметру своя колонка, «Сумма» — их сумма.
	advTotal, invTotal, advPaid, invPaid := moneyDocTotals(year.Rows)
	advDue := model.Round2(advTotal - advPaid)
	invDue := model.Round2(invTotal - invPaid)

	d.Sections = append(d.Sections, docgen.Section{
		Title: "Итого",
		Columns: []docgen.Column{
			docgen.Column{Title: "Показатель", Width: 48, Align: docgen.Left},
			docgen.Column{Title: "Аванс", Width: 34, Align: docgen.Right},
			docgen.Column{Title: "Счёт", Width: 34, Align: docgen.Right},
			docgen.Column{Title: "Сумма", Width: 34, Align: docgen.Right},
		},
		Rows: []docgen.Row{
			{Cells: []docgen.Cell{
				{Text: "Выставлено"},
				moneyDocAmount(advTotal),
				moneyDocAmount(invTotal),
				moneyDocAmount(advTotal + invTotal),
			}},
			{Cells: []docgen.Cell{
				{Text: "Оплачено"},
				moneyDocAmount(advPaid),
				moneyDocAmount(invPaid),
				moneyDocAmount(advPaid + invPaid),
			}},
			{Cells: []docgen.Cell{
				{Text: "К оплате"},
				moneyDocAmount(advDue),
				moneyDocAmount(invDue),
				moneyDocAmount(advDue + invDue),
			}},
		},
		// «К оплате» подсвечиваем фоном строки.
		HighlightLast: true,
	})
	return d
}

// moneyDocTotals суммирует выставленное и оплаченное по всем месяцам.
func moneyDocTotals(rows []moneyTableRowView) (advTotal, invTotal, advPaid, invPaid float64) {
	for _, row := range rows {
		advTotal += row.Advance.SumRaw
		invTotal += row.Invoice.SumRaw
		if row.Advance.Paid {
			advPaid += row.Advance.SumRaw
		}
		if row.Invoice.Paid {
			invPaid += row.Invoice.SumRaw
		}
	}
	return model.Round2(advTotal), model.Round2(invTotal),
		model.Round2(advPaid), model.Round2(invPaid)
}

// moneyDocAmount — сумма в итоговой таблице.
func moneyDocAmount(v float64) docgen.Cell {
	return docgen.Cell{Text: model.FormatMoney(v)}
}

// moneyDocSumMarked — сумма позиции; неоплаченная подчёркивается жирным.
// Так в документе сразу видно, что именно осталось неоплаченным.
func moneyDocSumMarked(c moneyCellView) docgen.Cell {
	cell := moneyDocSum(c)
	if c.HasSum && !c.Paid {
		cell.Strong = true
	}
	return cell
}
// moneyDocSum — сумма в колонке «Аванс»/«Счёт».
func moneyDocSum(c moneyCellView) docgen.Cell {
	if !c.HasSum {
		return docgen.Cell{Text: "—", Muted: true}
	}
	return docgen.Cell{Text: c.SumText + " ₽"}
}

// moneyDocPaid — оплата строки: «Оплачено», только если оплачены и
// аванс, и счёт; иначе «Не оплачено».
func moneyDocPaid(a, b moneyCellView) docgen.Cell {
	switch {
	case !a.HasSum && !b.HasSum:
		return docgen.Cell{Text: "—", Muted: true}
	case paidState(a) && paidState(b):
		return docgen.Cell{Text: "Оплачено"}
	}
	return docgen.Cell{Text: "Не оплачено"}
}

// paidState — позиция оплачена либо её нет вовсе.
func paidState(c moneyCellView) bool {
	return !c.HasSum || c.Paid
}

func dashText(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

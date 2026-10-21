package model

// Record — строка начисления за месяц. Хранится таблицей money:
// аванс — advance/advance_paid, счёт — charged/paid_flag.
type Record struct {
	Period      string // YYYY-M, как ключ записи
	AdvanceSum  float64
	AdvancePaid bool
	InvoiceSum  float64
	InvoicePaid bool
}

// Money собирает запись месяца в строку таблицы начислений.
func (m Money) Record() Record {
	return Record{
		Period:      m.Period,
		AdvanceSum:  Round2(m.AdvanceSum),
		AdvancePaid: m.AdvancePaid,
		InvoiceSum:  Round2(m.Charged),
		InvoicePaid: m.PaidFlag,
	}
}

// Paid — оплаченная часть: суммы позиций, отмеченных оплаченными.
func (r Record) Paid() float64 {
	sum := 0.0
	if r.AdvancePaid {
		sum += r.AdvanceSum
	}
	if r.InvoicePaid {
		sum += r.InvoiceSum
	}
	return Round2(sum)
}

// Due — к оплате: суммы обеих позиций минус оплаченное.
// Как в эталоне, позиция без отметки считается неоплаченной, даже если пустая.
func (r Record) Due() float64 {
	return Round2(Round2(r.AdvanceSum+r.InvoiceSum) - r.Paid())
}

// Total — «Итого» строки: аванс плюс счёт.
func (r Record) Total() float64 { return Round2(r.AdvanceSum + r.InvoiceSum) }

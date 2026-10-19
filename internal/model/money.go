package model

import "time"

// Account — лицевой счёт, по которому ведутся начисления.
type Account string

const (
	AccountWater     Account = "water" // старые данные
	AccountWaterCold Account = "water_cold"
	AccountWaterHot  Account = "water_hot"
	AccountEnergy    Account = "energy"
)

// AccountOrder — порядок отображения счетов.
var AccountOrder = []Account{AccountWaterCold, AccountWaterHot, AccountEnergy}

// ValidAccount проверяет счёт.
func ValidAccount(v string) bool {
	switch Account(v) {
	case AccountWater, AccountWaterCold, AccountWaterHot, AccountEnergy:
		return true
	}
	return false
}

// AccountFor возвращает счёт для ресурса.
func AccountFor(r Resource) Account {
	switch r {
	case Energy:
		return AccountEnergy
	case WaterHot:
		return AccountWaterHot
	}
	return AccountWaterCold
}

// Label — полное название счёта.
func (a Account) Label() string {
	switch a {
	case AccountWater, AccountWaterCold:
		return "Холодная вода"
	case AccountWaterHot:
		return "Горячая вода"
	case AccountEnergy:
		return "Электроэнергия"
	}
	return string(a)
}

// Short — короткое название.
func (a Account) Short() string {
	switch a {
	case AccountWater, AccountWaterCold:
		return "ХВС"
	case AccountWaterHot:
		return "ГВС"
	case AccountEnergy:
		return "Электро"
	}
	return string(a)
}

// Resource возвращает основной ресурс счёта (для иконки).
func (a Account) Resource() Resource {
	switch a {
	case AccountEnergy:
		return Energy
	case AccountWaterHot:
		return WaterHot
	}
	return WaterCold
}

// Money — начисления и оплаты по счёту за месяц.
type Money struct {
	ObjectID    int64
	Period      string
	Account     Account
	Charged     float64 // выставленный счёт
	Paid        float64 // всего оплачено: аванс + счёт, если отмечен оплаченным
	AdvanceSum  float64 // аванс
	AdvancePaid bool    // аванс отмечен оплаченным
	PaidFlag    bool    // счёт отмечен оплаченным
	Note        string
	UpdatedAt   time.Time
}

// Debt — остаток по счёту: счёт минус оплаченный аванс.
// Если счёт отмечен оплаченным, долга нет.
func (m Money) Debt() float64 {
	if m.Charged <= 0 || m.PaidFlag {
		return 0
	}
	paid := 0.0
	if m.AdvancePaid {
		paid = m.AdvanceSum
	}
	if paid >= m.Charged {
		return 0
	}
	return Round3(m.Charged - paid)
}

// HasValues сообщает, что по счёту есть суммы.
func (m Money) HasValues() bool { return m.Charged != 0 || m.Paid != 0 || m.AdvanceSum != 0 }

// IsPaid сообщает, отмечен ли счёт оплаченным.
func (m Money) IsPaid() bool { return m.PaidFlag || (m.Charged > 0 && m.Paid >= m.Charged) }

// Advance — сумма аванса/переплаты.
func (m Money) Advance() float64 { return Round2(m.AdvanceSum) }

// Overpay — переплата сверх счёта: аванс плюс остаток от ручной оплаты.
func (m Money) Overpay() float64 {
	if m.PaidFlag || m.Charged <= 0 {
		return Round2(m.AdvanceSum)
	}
	if m.Paid > m.Charged {
		return Round2(m.Paid - m.Charged)
	}
	return 0
}

// FormatMoney — «1 234,50 ₽».
func FormatMoney(v float64) string {
	return FormatValue(Round2(v)) + " ₽"
}

// Round2 округляет деньги до копеек.
func Round2(v float64) float64 {
	if v < 0 {
		return -Round2(-v)
	}
	return float64(int64(v*100+0.5)) / 100
}

// MoneyLabel — подпись остатка: «Долг», «Аванс» или «Нет долга».
func MoneyLabel(debt float64) string {
	switch {
	case debt > 0:
		return "Долг"
	case debt < 0:
		return "Аванс"
	}
	return "Без долга"
}

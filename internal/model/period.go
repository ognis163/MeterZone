package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Period — календарный месяц, за который вносятся показания.
type Period struct {
	Year  int
	Month int // 1..12
}

var monthNames = [...]string{
	"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
	"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
}

var monthNamesGenitive = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

var monthNamesShort = [...]string{
	"янв", "фев", "мар", "апр", "май", "июн",
	"июл", "авг", "сен", "окт", "ноя", "дек",
}

// NewPeriod создаёт период с проверкой месяца.
func NewPeriod(year, month int) (Period, error) {
	if year < 2000 || year > 2100 {
		return Period{}, fmt.Errorf("год вне допустимого диапазона")
	}
	if month < 1 || month > 12 {
		return Period{}, fmt.Errorf("месяц должен быть от 1 до 12")
	}
	return Period{Year: year, Month: month}, nil
}

// ParsePeriod разбирает строку вида 2026-10.
func ParsePeriod(s string) (Period, error) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[4] != '-' {
		return Period{}, fmt.Errorf("период должен быть в формате ГГГГ-ММ")
	}
	year, err := strconv.Atoi(s[:4])
	if err != nil {
		return Period{}, fmt.Errorf("некорректный год")
	}
	month, err := strconv.Atoi(s[5:])
	if err != nil {
		return Period{}, fmt.Errorf("некорректный месяц")
	}
	return NewPeriod(year, month)
}

// PeriodOf возвращает период даты.
func PeriodOf(t time.Time) Period { return Period{Year: t.Year(), Month: int(t.Month())} }

// String — формат хранения: 2026-10.
func (p Period) String() string { return fmt.Sprintf("%04d-%02d", p.Year, p.Month) }

// Valid сообщает, что период заполнен корректно.
func (p Period) Valid() bool {
	return p.Year >= 2000 && p.Year <= 2100 && p.Month >= 1 && p.Month <= 12
}

// Label — «Октябрь 2026».
func (p Period) Label() string {
	if !p.Valid() {
		return ""
	}
	return monthNames[p.Month-1] + " " + strconv.Itoa(p.Year)
}

// ShortLabel — «окт 2026».
func (p Period) ShortLabel() string {
	if !p.Valid() {
		return ""
	}
	return monthNamesShort[p.Month-1] + " " + strconv.Itoa(p.Year)
}

// MonthShort — «окт».
func (p Period) MonthShort() string {
	if !p.Valid() {
		return ""
	}
	return monthNamesShort[p.Month-1]
}

// MonthName — «Октябрь».
func (p Period) MonthName() string {
	if !p.Valid() {
		return ""
	}
	return monthNames[p.Month-1]
}

// Genitive — «октября 2026».
func (p Period) Genitive() string {
	if !p.Valid() {
		return ""
	}
	return monthNamesGenitive[p.Month-1] + " " + strconv.Itoa(p.Year)
}

// Accusative — «октябрь 2026» (для «за октябрь 2026 г.»).
func (p Period) Accusative() string {
	if !p.Valid() {
		return ""
	}
	return strings.ToLower(monthNames[p.Month-1]) + " " + strconv.Itoa(p.Year)
}

// Add сдвигает период на n месяцев.
func (p Period) Add(n int) Period {
	total := p.Year*12 + (p.Month - 1) + n
	y := total / 12
	m := total%12 + 1
	if m < 1 {
		m += 12
		y--
	}
	return Period{Year: y, Month: m}
}

// Prev — предыдущий месяц.
func (p Period) Prev() Period { return p.Add(-1) }

// Next — следующий месяц.
func (p Period) Next() Period { return p.Add(1) }

// Before сообщает, что период раньше другого.
func (p Period) Before(o Period) bool {
	return p.Year < o.Year || (p.Year == o.Year && p.Month < o.Month)
}

// After сообщает, что период позже другого.
func (p Period) After(o Period) bool { return o.Before(p) }

// Compare возвращает -1, 0 или 1.
func (p Period) Compare(o Period) int {
	switch {
	case p.Before(o):
		return -1
	case p.After(o):
		return 1
	}
	return 0
}

// MonthDiff — число месяцев от p до o (может быть отрицательным).
func (p Period) MonthDiff(o Period) int {
	return (o.Year-p.Year)*12 + (o.Month - p.Month)
}

// MonthsBetween возвращает список месяцев включительно.
func MonthsBetween(from, to Period) []Period {
	if to.Before(from) {
		return nil
	}
	n := from.MonthDiff(to)
	out := make([]Period, 0, n+1)
	for i := 0; i <= n; i++ {
		out = append(out, from.Add(i))
	}
	return out
}

// PeriodNow возвращает текущий месяц в указанной зоне.
func PeriodNow(loc *time.Location) Period {
	if loc == nil {
		loc = time.Local
	}
	return PeriodOf(time.Now().In(loc))
}

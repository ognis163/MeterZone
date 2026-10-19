package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Round3 округляет значение до трёх знаков после запятой.
func Round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// FormatValue форматирует число для интерфейса и документов:
// десятичная запятая, пробелы между разрядами, без лишних нулей.
func FormatValue(v float64) string {
	v = Round3(v)
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := int64(math.Round((v - float64(whole)) * 1000))
	if frac >= 1000 {
		whole++
		frac -= 1000
	}
	out := formatThousands(whole)
	if frac > 0 {
		s := fmt.Sprintf("%03d", frac)
		s = strings.TrimRight(s, "0")
		out += "," + s
	}
	if neg {
		out = "−" + out
	}
	return out
}

func formatThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	first := len(s) % 3
	if first > 0 {
		b.WriteString(s[:first])
	}
	for i := first; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// FormatInput форматирует число для поля ввода (без разрядов, с запятой).
func FormatInput(v float64) string {
	v = Round3(v)
	if v == 0 {
		return ""
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	return strings.ReplaceAll(s, ".", ",")
}

// ParseValue разбирает введённое пользователем показание.
// Допускаются запятая и точка, пробелы; отрицательные и слишком большие
// значения отклоняются.
func ParseValue(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("пустое значение")
	}
	s = strings.ReplaceAll(s, "\u00a0", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	if strings.ContainsAny(s, "eE+-") {
		return 0, fmt.Errorf("недопустимые символы")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("введите число")
	}
	if v < 0 {
		return 0, fmt.Errorf("значение не может быть отрицательным")
	}
	if v > 9_999_999 {
		return 0, fmt.Errorf("слишком большое значение")
	}
	return Round3(v), nil
}

// FormatDateRu — «19.10.2026».
func FormatDateRu(t time.Time) string { return t.Format("02.01.2006") }

// FormatDateTimeRu — «19.10.2026, 04:05».
func FormatDateTimeRu(t time.Time) string { return t.Format("02.01.2006, 15:04") }

// PluralRu выбирает форму слова по числу: 1 объект, 2 объекта, 5 объектов.
func PluralRu(n int, one, few, many string) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs%100 >= 11 && abs%100 <= 14:
		return many
	case abs%10 == 1:
		return one
	case abs%10 >= 2 && abs%10 <= 4:
		return few
	}
	return many
}

// Consumption возвращает разницу показаний и признак корректности.
func Consumption(prev, curr float64) (float64, bool) {
	d := Round3(curr - prev)
	if d < 0 {
		return 0, false
	}
	return d, true
}

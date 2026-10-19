package model

import "testing"

func TestPeriodParseAndFormat(t *testing.T) {
	p, err := ParsePeriod("2026-10")
	if err != nil {
		t.Fatalf("ParsePeriod: %v", err)
	}
	if p.Year != 2026 || p.Month != 10 {
		t.Fatalf("получен %+v", p)
	}
	if got := p.String(); got != "2026-10" {
		t.Fatalf("String = %q", got)
	}
	if got := p.Label(); got != "Октябрь 2026" {
		t.Fatalf("Label = %q", got)
	}
	if got := p.Accusative(); got != "октябрь 2026" {
		t.Fatalf("Accusative = %q", got)
	}
	if got := p.Genitive(); got != "октября 2026" {
		t.Fatalf("Genitive = %q", got)
	}

	for _, bad := range []string{"", "2026-13", "2026-00", "26-10", "2026/10", "abcd-ef"} {
		if _, err := ParsePeriod(bad); err == nil {
			t.Fatalf("ожидалась ошибка для %q", bad)
		}
	}
}

func TestPeriodArithmetic(t *testing.T) {
	p := Period{Year: 2026, Month: 1}
	if got := p.Prev().String(); got != "2025-12" {
		t.Fatalf("Prev = %s", got)
	}
	if got := p.Next().String(); got != "2026-02" {
		t.Fatalf("Next = %s", got)
	}
	if got := (Period{2024, 2}).Add(12).String(); got != "2025-02" {
		t.Fatalf("Add(12) = %s", got)
	}
	if got := (Period{2026, 1}).MonthDiff(Period{2027, 3}); got != 14 {
		t.Fatalf("MonthDiff = %d", got)
	}
	months := MonthsBetween(Period{2025, 11}, Period{2026, 2})
	if len(months) != 4 || months[0].String() != "2025-11" || months[3].String() != "2026-02" {
		t.Fatalf("MonthsBetween = %v", months)
	}
	if !(Period{2026, 1}).Before(Period{2026, 2}) {
		t.Fatal("Before")
	}
	if (Period{2026, 2}).Before(Period{2026, 2}) {
		t.Fatal("Before same")
	}
}

func TestFormatAndParseValue(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{3.2, "3,2"},
		{164.8, "164,8"},
		{2048, "2 048"},
		{1234567.891, "1 234 567,891"},
		{0.5, "0,5"},
		{12.0004, "12"},
	}
	for _, c := range cases {
		if got := FormatValue(c.in); got != c.want {
			t.Errorf("FormatValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}

	good := map[string]float64{
		"3,2":      3.2,
		" 1 234,5": 1234.5,
		"0":        0,
		"12.25":    12.25,
		"004,500":  4.5,
	}
	for raw, want := range good {
		got, err := ParseValue(raw)
		if err != nil {
			t.Errorf("ParseValue(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("ParseValue(%q) = %v, want %v", raw, got, want)
		}
	}

	for _, bad := range []string{"", "abc", "-1", "1e5", "1+2", "99999999"} {
		if _, err := ParseValue(bad); err == nil {
			t.Errorf("ожидалась ошибка для %q", bad)
		}
	}
}

func TestFormatInput(t *testing.T) {
	if got := FormatInput(164.8); got != "164,8" {
		t.Fatalf("FormatInput = %q", got)
	}
	if got := FormatInput(0); got != "" {
		t.Fatalf("FormatInput(0) = %q", got)
	}
}

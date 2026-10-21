package docgen

import (
	"bytes"
	"math"
	"os"
	"regexp"
	"testing"
	"time"

	"indextro/internal/model"
	"indextro/internal/report"
)

func loadFont(t *testing.T) []byte {
	t.Helper()
	font, err := os.ReadFile("../../DejaVuSans.ttf")
	if err != nil {
		t.Fatalf("шрифт: %v", err)
	}
	return font
}

func sampleMonth() report.Month {
	obj := model.Object{ID: 1, Name: "Дом", Address: "ул. Примерная, 1", Owner: "Владелец",
		AccountWater: "123", AccountEnergy: "456"}
	m1 := model.Meter{ID: 10, ObjectID: 1, Resource: model.WaterCold, Label: "Кухня", Brand: "СВК-15", Serial: "118402"}
	m2 := model.Meter{ID: 11, ObjectID: 1, Resource: model.Energy, Label: "Щиток", Serial: "09318452"}
	return report.Month{
		Object:    obj,
		Period:    model.Period{Year: 2026, Month: 10},
		Scope:     "all",
		Generated: time.Now(),
		Sections: []report.MonthSection{
			{
				Resource: model.WaterCold, Title: model.WaterCold.Section(), Unit: "куб.м",
				Rows: []report.MonthRow{
					{Meter: m1, Prev: 161.6, Curr: 164.8, Diff: 3.2, HasPrev: true, HasCurr: true, HasDiff: true},
				},
				Total: 3.2,
			},
			{
				Resource: model.Energy, Title: model.Energy.Section(), Unit: "кВт·ч",
				Rows: []report.MonthRow{
					{Meter: m2, Prev: 2048, Curr: 2094, Diff: 46, HasPrev: true, HasCurr: true, HasDiff: true},
				},
				Total: 46,
			},
		},
		HasData:     true,
		HasReadings: true,
	}
}

func TestMonthDocument(t *testing.T) {
	doc := Month(sampleMonth())
	if doc.Title != "Сведения о показаниях" {
		t.Fatalf("заголовок: %q", doc.Title)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("разделов %d, ожидалось 2", len(doc.Sections))
	}
	if doc.Sections[0].Title != "Холодное водоснабжение" {
		t.Fatalf("раздел: %q", doc.Sections[0].Title)
	}
	if got := doc.Sections[0].Rows[0].Cells[0].Text; got != "Кухня" {
		t.Fatalf("место установки: %q", got)
	}
	// Сумма колонок ровно равна полосе набора — таблица не должна
	// вылезать за поля и оставлять пустой хвост.
	for _, sec := range doc.Sections {
		var sum float64
		for _, c := range sec.Columns {
			sum += c.Width
		}
		if math.Abs(sum-ContentW) > 0.01 {
			t.Fatalf("%s: ширина колонок %.2f, ожидалось %.2f", sec.Title, sum, ContentW)
		}
		for _, row := range sec.Rows {
			if len(row.Cells) != len(sec.Columns) {
				t.Fatalf("%s: ячеек %d, колонок %d", sec.Title, len(row.Cells), len(sec.Columns))
			}
		}
	}
}

func TestMonthEmptyDocument(t *testing.T) {
	rep := sampleMonth()
	rep.Sections = nil
	rep.HasReadings = false
	doc := Month(rep)
	if doc.Empty == "" {
		t.Fatal("нет сообщения о пустом месяце")
	}
	if len(doc.Sections) != 0 {
		t.Fatal("в пустом документе не должно быть таблиц")
	}
}

func sampleRange() report.Range {
	rep := report.Range{
		Object:    model.Object{ID: 1, Name: "Дом", Address: "ул. Примерная, 1", Owner: "Владелец"},
		From:      model.Period{Year: 2025, Month: 1},
		To:        model.Period{Year: 2026, Month: 12},
		Scope:     "all",
		Generated: time.Now(),
		Resources: []model.Resource{model.WaterCold, model.Energy},
		ResourceTotals: map[model.Resource]float64{
			model.WaterCold: 41.6,
			model.Energy:    598,
		},
		HasData: true,
	}
	m1 := model.Meter{ID: 10, Resource: model.WaterCold, Label: "Кухня", Brand: "СВК-15", Serial: "118402"}
	m2 := model.Meter{ID: 11, Resource: model.Energy, Label: "Щиток", Serial: "09318452"}
	rep.MeterTotals = []report.MeterTotal{
		{Meter: m1, Total: 41.6, HasTotal: true, LastValue: 164.8, HasLast: true},
		{Meter: m2, Total: 598, HasTotal: true, LastValue: 2094, HasLast: true},
	}
	for p := rep.From; !p.After(rep.To); p = p.Next() {
		rep.Rows = append(rep.Rows, report.RangeRow{
			Period: p,
			Cells: []report.RangeCell{
				{Resource: model.WaterCold, Reading: 164.8, HasReading: true, Consumption: 3.2, Has: true},
				{Resource: model.Energy, Reading: 2094, HasReading: true, Consumption: 46, Has: true},
			},
		})
	}
	return rep
}

func TestRangeDocument(t *testing.T) {
	doc := Range(sampleRange())
	if len(doc.Sections) != 2 {
		t.Fatalf("разделов %d, ожидалось 2", len(doc.Sections))
	}
	readings := doc.Sections[0]
	if len(readings.Columns) != 5 {
		t.Fatalf("колонок %d, ожидалось 5", len(readings.Columns))
	}
	if len(readings.Total) != len(readings.Columns) {
		t.Fatalf("итог: %d ячеек, колонок %d", len(readings.Total), len(readings.Columns))
	}
	if readings.Total[0].Text != "Итого" {
		t.Fatalf("итог: %q", readings.Total[0].Text)
	}
	if doc.Sections[1].Title != "Приборы учёта" {
		t.Fatalf("второй раздел: %q", doc.Sections[1].Title)
	}
}

func TestMonthPDF(t *testing.T) {
	data, err := PDF(Month(sampleMonth()), loadFont(t))
	if err != nil {
		t.Fatalf("PDF: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatal("это не PDF")
	}
	if len(data) < 8000 {
		t.Fatalf("подозрительно маленький PDF: %d байт", len(data))
	}
	// gpdf встраивает полный Type0-шрифт с картой кодов в Unicode, поэтому
	// кириллица остаётся доступной для поиска и копирования.
	for _, marker := range []string{"/Type0", "/ToUnicode", "/FontFile2", "/CIDFontType2"} {
		if !bytes.Contains(data, []byte(marker)) {
			t.Errorf("в PDF нет %s", marker)
		}
	}
}

func TestEmptyMonthPDF(t *testing.T) {
	data, err := PDF(Month(report.Month{
		Object:    model.Object{ID: 1, Address: "ул. Примерная, 1"},
		Period:    model.Period{Year: 2026, Month: 10},
		Generated: time.Now(),
	}), loadFont(t))
	if err != nil {
		t.Fatalf("PDF: %v", err)
	}
	if len(data) < 2000 {
		t.Fatalf("пустой документ слишком мал: %d", len(data))
	}
}

func TestRangePDFPages(t *testing.T) {
	// Пять лет по месяцам не помещаются на лист: таблица должна перейти
	// на следующую страницу вместе с шапкой, а не обрезаться.
	rep := sampleRange()
	rep.From = rep.To.Add(-12 * 5)
	// На каждый месяц — шесть строк, чтобы таблица точно не влезла на лист.
	rep.Rows = nil
	for p := rep.From; !p.After(rep.To); p = p.Next() {
		cells := []report.RangeCell{
			{Resource: model.WaterCold, Reading: 164.8, HasReading: true, Consumption: 3.2, Has: true},
			{Resource: model.Energy, Reading: 2094, HasReading: true, Consumption: 46, Has: true},
		}
		for i := 0; i < 6; i++ {
			rep.Rows = append(rep.Rows, report.RangeRow{Period: p, Cells: cells})
		}
	}
	data, err := PDF(Range(rep), loadFont(t))
	if err != nil {
		t.Fatalf("PDF: %v", err)
	}
	pages := regexp.MustCompile(`/Type\s*/Page[^s]`).FindAll(data, -1)
	if len(pages) < 2 {
		t.Fatalf("страниц %d, ожидалось минимум 2", len(pages))
	}
}
func TestRangeSubtitle(t *testing.T) {
	from := model.Period{Year: 2025, Month: 1}
	to := model.Period{Year: 2026, Month: 12}
	got := rangeSubtitle(from, to)
	if got != "за период с января 2025 по декабрь 2026" {
		t.Fatalf("подзаголовок: %q", got)
	}
	if one := rangeSubtitle(to, to); one != "за декабрь 2026 г." {
		t.Fatalf("один месяц: %q", one)
	}
}

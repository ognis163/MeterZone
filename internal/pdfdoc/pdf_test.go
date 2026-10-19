package pdfdoc

import (
	"bytes"
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
		HasData: true,
	}
}

func TestMonthPDF(t *testing.T) {
	font := loadFont(t)
	data, err := MonthPDF(sampleMonth(), font)
	if err != nil {
		t.Fatalf("MonthPDF: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatal("это не PDF")
	}
	if len(data) < 8000 {
		t.Fatalf("подозрительно маленький PDF: %d байт", len(data))
	}
	if !bytes.Contains(data, []byte("beginbfchar")) {
		t.Error("нет корректной таблицы ToUnicode")
	}
	if bytes.Contains(data, []byte("{nb}")) {
		t.Error("не подставлено общее число страниц")
	}
	if !bytes.Contains(data, []byte("/FontFile2")) {
		t.Error("шрифт не встроен")
	}
}

func TestEmptyMonthPDF(t *testing.T) {
	font := loadFont(t)
	rep := sampleMonth()
	rep.Sections = nil
	data, err := MonthPDF(rep, font)
	if err != nil {
		t.Fatalf("MonthPDF: %v", err)
	}
	if len(data) < 2000 {
		t.Fatalf("пустой документ слишком мал: %d", len(data))
	}
}

func TestRangePDFPages(t *testing.T) {
	font := loadFont(t)
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
				{Resource: model.WaterCold, Consumption: 3.2, Has: true},
				{Resource: model.Energy, Consumption: 46, Has: true},
			},
		})
	}
	data, err := RangePDF(rep, font)
	if err != nil {
		t.Fatalf("RangePDF: %v", err)
	}
	pages := regexp.MustCompile(`/Type\s*/Page[^s]`).FindAll(data, -1)
	if len(pages) < 2 {
		t.Fatalf("страниц %d, ожидалось минимум 2", len(pages))
	}
	if bytes.Contains(data, []byte("{nb}")) {
		t.Error("не подставлено общее число страниц")
	}
}

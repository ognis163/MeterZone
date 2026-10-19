package report

import (
	"testing"
	"time"

	"indextro/internal/model"
)

func testData() ObjectData {
	obj := model.Object{ID: 1, Name: "Дом"}
	m1 := model.Meter{ID: 10, ObjectID: 1, Resource: model.WaterCold, Label: "Кухня", StartValue: 90}
	m2 := model.Meter{ID: 11, ObjectID: 1, Resource: model.WaterCold, Label: "Ванная", StartValue: 40}
	m3 := model.Meter{ID: 12, ObjectID: 1, Resource: model.Energy, Label: "Щиток", StartValue: 900}
	return ObjectData{
		Object: obj,
		Meters: []model.Meter{m1, m2, m3},
		Readings: map[int64]map[string]float64{
			10: {"2026-08": 100, "2026-09": 110, "2026-10": 120},
			11: {"2026-09": 50, "2026-10": 55},
			12: {"2026-10": 1000},
		},
	}
}

func TestMonthReport(t *testing.T) {
	od := testData()
	rep := MonthReport(od, model.Period{Year: 2026, Month: 10}, "all", time.Now())
	if len(rep.Sections) != 2 {
		t.Fatalf("разделов %d", len(rep.Sections))
	}
	water := rep.Sections[0]
	if water.Resource != model.WaterCold || len(water.Rows) != 2 {
		t.Fatalf("вода: %+v", water)
	}
	if water.Total != 15 {
		t.Fatalf("итого по воде %v, ожидалось 15", water.Total)
	}
	for _, row := range water.Rows {
		if !row.HasPrev || !row.HasCurr || !row.HasDiff {
			t.Fatalf("неполная строка: %+v", row)
		}
	}
	if water.Rows[0].Diff != 10 || water.Rows[1].Diff != 5 {
		t.Fatalf("расходы: %v, %v", water.Rows[0].Diff, water.Rows[1].Diff)
	}

	energy := rep.Sections[1]
	if len(energy.Rows) != 1 {
		t.Fatalf("электро: %+v", energy.Rows)
	}
	if !energy.Rows[0].HasPrev || energy.Rows[0].Prev != 900 {
		t.Fatalf("начальное показание не учтено: %+v", energy.Rows[0])
	}
	if energy.Rows[0].Diff != 100 {
		t.Fatalf("расход электро %v, ожидалось 100", energy.Rows[0].Diff)
	}
}

func TestMonthReportScope(t *testing.T) {
	od := testData()
	rep := MonthReport(od, model.Period{Year: 2026, Month: 10}, "water", time.Now())
	if len(rep.Sections) != 1 || rep.Sections[0].Resource != model.WaterCold {
		t.Fatalf("фильтр по воде: %+v", rep.Sections)
	}
	rep = MonthReport(od, model.Period{Year: 2026, Month: 10}, "energy", time.Now())
	if len(rep.Sections) != 1 || rep.Sections[0].Resource != model.Energy {
		t.Fatalf("фильтр по свету: %+v", rep.Sections)
	}
}

func TestMonthStartValue(t *testing.T) {
	od := testData()
	// Первое показание по прибору: базой служит начальное значение.
	rep := MonthReport(od, model.Period{Year: 2026, Month: 8}, "all", time.Now())
	section := rep.Sections[0]
	if len(section.Rows) != 2 {
		t.Fatalf("строк %d, ожидалось 2 (второй прибор без показания)", len(section.Rows))
	}
	var row MonthRow
	for _, r := range section.Rows {
		if r.Meter.ID == 10 {
			row = r
		}
	}
	if !row.HasPrev || row.Prev != 90 {
		t.Fatalf("предыдущее должно быть 90: %+v", row)
	}
	if row.Diff != 10 {
		t.Fatalf("расход %v", row.Diff)
	}
}

func TestArchivedMeterRules(t *testing.T) {
	od := testData()
	arch := model.Meter{ID: 20, ObjectID: 1, Resource: model.WaterHot, Label: "Архив", Archived: true}
	od.Meters = append(od.Meters, arch)
	// Нет показания за месяц — архивный прибор не попадает в отчёт.
	rep := MonthReport(od, model.Period{Year: 2026, Month: 10}, "all", time.Now())
	for _, s := range rep.Sections {
		for _, r := range s.Rows {
			if r.Meter.ID == 20 {
				t.Fatal("архивный прибор без показания попал в отчёт")
			}
		}
	}
	// Есть показание — попадает, чтобы не терять историю.
	od.Readings[20] = map[string]float64{"2026-10": 5}
	rep = MonthReport(od, model.Period{Year: 2026, Month: 10}, "all", time.Now())
	found := false
	for _, s := range rep.Sections {
		for _, r := range s.Rows {
			if r.Meter.ID == 20 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("архивный прибор с показанием не попал в отчёт")
	}
}

func TestRangeReport(t *testing.T) {
	od := testData()
	rep := RangeReport(od, model.Period{Year: 2026, Month: 9}, model.Period{Year: 2026, Month: 11}, "all", time.Now())
	if len(rep.Rows) != 3 {
		t.Fatalf("строк %d", len(rep.Rows))
	}
	if len(rep.Resources) != 2 {
		t.Fatalf("ресурсов %d", len(rep.Resources))
	}
	// Сентябрь: холодная 10 + 10 = 20, света нет.
	if !rep.Rows[0].Cells[0].Has || rep.Rows[0].Cells[0].Consumption != 20 {
		t.Fatalf("сентябрь: %+v", rep.Rows[0].Cells)
	}
	if rep.Rows[0].Cells[1].Has {
		t.Fatal("в сентябре не должно быть электроэнергии")
	}
	// Октябрь: холодная 10 + 5 = 15, электро 100 (от начального 900).
	if rep.Rows[1].Cells[0].Consumption != 15 || rep.Rows[1].Cells[1].Consumption != 100 {
		t.Fatalf("октябрь: %+v", rep.Rows[1].Cells)
	}
	// Ноябрь без данных.
	if rep.Rows[2].Cells[0].Has || rep.Rows[2].Cells[1].Has {
		t.Fatalf("ноябрь: %+v", rep.Rows[2].Cells)
	}
	if rep.ResourceTotals[model.WaterCold] != 35 {
		t.Fatalf("итого вода %v", rep.ResourceTotals[model.WaterCold])
	}
	if rep.ResourceTotals[model.Energy] != 100 {
		t.Fatalf("итого свет %v", rep.ResourceTotals[model.Energy])
	}
	if len(rep.MeterTotals) != 3 {
		t.Fatalf("итоги по приборам: %d", len(rep.MeterTotals))
	}
}

func TestPrevReading(t *testing.T) {
	series := map[string]float64{"2026-01": 10, "2026-03": 30, "2026-05": 50}
	v, p, ok := PrevReading(series, model.Period{Year: 2026, Month: 4})
	if !ok || v != 30 || p.String() != "2026-03" {
		t.Fatalf("PrevReading = %v %v %v", v, p, ok)
	}
	if _, _, ok := PrevReading(series, model.Period{Year: 2026, Month: 1}); ok {
		t.Fatal("до первого показания ничего быть не должно")
	}
	v, p, ok = LastReadingUpto(series, model.Period{Year: 2026, Month: 4})
	if !ok || v != 30 || p.String() != "2026-03" {
		t.Fatalf("LastReadingUpto = %v %v %v", v, p, ok)
	}
}

func TestStatusFor(t *testing.T) {
	od := testData()
	st := StatusFor(od, model.Period{Year: 2026, Month: 10})
	if st.Total != 3 || st.Filled != 3 || !st.Complete() {
		t.Fatalf("статус: %+v", st)
	}
	st = StatusFor(od, model.Period{Year: 2026, Month: 11})
	if st.Filled != 0 || st.Complete() {
		t.Fatalf("статус пустого месяца: %+v", st)
	}
}

func TestMonthNoBaseline(t *testing.T) {
	m := model.Meter{ID: 30, ObjectID: 1, Resource: model.Energy, Label: "Новый"}
	od := ObjectData{
		Object:   model.Object{ID: 1},
		Meters:   []model.Meter{m},
		Readings: map[int64]map[string]float64{30: {"2026-10": 15}},
	}
	rep := MonthReport(od, model.Period{Year: 2026, Month: 10}, "all", time.Now())
	row := rep.Sections[0].Rows[0]
	if row.HasPrev || row.HasDiff {
		t.Fatalf("расход без базового показания: %+v", row)
	}
	if !row.HasCurr || row.Curr != 15 {
		t.Fatalf("текущее показание: %+v", row)
	}
}

func TestNextReading(t *testing.T) {
	series := map[string]float64{"2026-01": 100, "2026-04": 130}
	v, p, ok := NextReading(series, model.Period{Year: 2026, Month: 2})
	if !ok || v != 130 || p.Month != 4 {
		t.Fatalf("NextReading = %v %v %v", v, p, ok)
	}
	if _, _, ok := NextReading(series, model.Period{Year: 2026, Month: 4}); ok {
		t.Fatal("после последнего показания не должно быть следующего")
	}
}

package report

import (
	"testing"
	"time"

	"indextro/internal/model"
)

func subData() ObjectData {
	parent := model.Meter{ID: 10, ObjectID: 1, Resource: model.WaterCold, Label: "Основной", StartValue: 90}
	child := model.Meter{ID: 11, ObjectID: 1, Resource: model.WaterCold, Label: "Полив", ParentID: 10, StartValue: 39}
	return ObjectData{
		Object: model.Object{ID: 1, Name: "Дом"},
		Meters: []model.Meter{parent, child},
		Readings: map[int64]map[string]float64{
			10: {"2026-08": 100, "2026-09": 110},
			11: {"2026-08": 40, "2026-09": 43},
		},
	}
}

func TestSubMeterMonth(t *testing.T) {
	od := subData()
	rep := MonthReport(od, model.Period{Year: 2026, Month: 9}, "all", time.Now())
	if !rep.HasSub {
		t.Fatal("нет признака суб-счётчика")
	}
	if len(rep.Sections) != 1 || len(rep.Sections[0].Rows) != 2 {
		t.Fatalf("строки: %+v", rep.Sections)
	}
	parent := rep.Sections[0].Rows[0]
	child := rep.Sections[0].Rows[1]
	if parent.Net || parent.Diff != 10 || parent.SubTotal != 0 || parent.SubCount != 0 {
		t.Fatalf("основной прибор: %+v", parent)
	}
	if !child.IsSub || child.Diff != 3 {
		t.Fatalf("суб-счётчик: %+v", child)
	}
	if rep.Sections[0].Total != 10 {
		t.Fatalf("итог раздела %v, ожидалось 10", rep.Sections[0].Total)
	}
}

func TestSubMeterRange(t *testing.T) {
	od := subData()
	rep := RangeReport(od, model.Period{Year: 2026, Month: 8}, model.Period{Year: 2026, Month: 9}, "all", time.Now())
	if rep.ResourceTotals[model.WaterCold] != 20 {
		t.Fatalf("итого вода %v, ожидалось 20", rep.ResourceTotals[model.WaterCold])
	}
	if rep.Rows[0].Cells[0].Consumption != 10 || rep.Rows[1].Cells[0].Consumption != 10 {
		t.Fatalf("по месяцам: %+v, %+v", rep.Rows[0].Cells[0], rep.Rows[1].Cells[0])
	}
	if len(rep.MeterTotals) != 2 || !rep.MeterTotals[1].IsSub {
		t.Fatalf("итоги по приборам: %+v", rep.MeterTotals)
	}
}

func TestConsumptionFor(t *testing.T) {
	od := subData()
	parent := od.Meters[0]
	child := od.Meters[1]
	if v, ok := od.ConsumptionFor(parent, model.Period{Year: 2026, Month: 9}); !ok || v != 10 {
		t.Fatalf("расход основного %v %v", v, ok)
	}
	if v, ok := od.ConsumptionFor(child, model.Period{Year: 2026, Month: 9}); !ok || v != 3 {
		t.Fatalf("расход суб %v %v", v, ok)
	}
}

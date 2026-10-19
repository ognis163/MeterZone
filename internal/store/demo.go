package store

import (
	"fmt"
	"time"

	"indextro/internal/model"
)

// SeedDemo создаёт два демонстрационных объекта с приборами, показаниями,
// суб-счётчиком и начислениями за последние 14 месяцев.
// Используется флагом -demo и в тестах.
func (s *Store) SeedDemo() error {
	count := 0
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM objects`).Scan(&count)
	if count > 0 {
		return fmt.Errorf("в базе уже есть объекты")
	}
	now := time.Now().In(s.loc)
	end := model.PeriodOf(now)
	start := end.Add(-13)

	type seedMeter struct {
		res    model.Resource
		label  string
		brand  string
		serial string
		base   float64
		step   float64
	}
	objects := []struct {
		obj    model.Object
		meters []seedMeter
		from   model.Period
	}{
		{
			obj: model.Object{
				Name:           "Дом",
				Kind:           model.KindHouse,
				Address:        "ул. Примерная, 1, кв. 1",
				Owner:          "Владелец",
				AccountWater:   "00000001",
				AccountEnergy:  "00000002",
				EmailWater:     "vodokanal@example.ru",
				EmailEnergy:    "energo@example.ru",
				FinanceEnabled: true,
				Note:           "Демонстрационные данные",
			},
			meters: []seedMeter{
				{model.WaterCold, "Кухня", "СВК-15", "118402", 120, 3.2},
				{model.WaterHot, "Кухня", "СВК-15", "118403", 78, 2.1},
				{model.Energy, "Щиток в прихожей", "Меркурий 201", "09318452", 1450, 46},
			},
			from: start,
		},
		{
			obj: model.Object{
				Name:           "Магазин",
				Kind:           model.KindCommercial,
				Address:        "ул. Примерная, 2, помещение 1",
				Owner:          "Организация",
				AccountWater:   "00000003",
				AccountEnergy:  "00000004",
				EmailWater:     "buh@example.ru",
				EmailEnergy:    "energo@example.ru",
				FinanceEnabled: true,
			},
			meters: []seedMeter{
				{model.WaterCold, "Вход, санузел", "ВС-20", "774120", 48, 4.4},
				{model.Energy, "Электрощит", "ТОМЗЭЛ 310", "552013", 3200, 210},
			},
			from: start.Add(4),
		},
	}

	for oi, def := range objects {
		obj := def.obj
		obj.CreatedAt = now
		if err := s.SaveObject(&obj); err != nil {
			return err
		}
		var firstCold int64
		for i, sm := range def.meters {
			m := model.Meter{
				ObjectID:       obj.ID,
				Resource:       sm.res,
				Label:          sm.label,
				Brand:          sm.brand,
				Serial:         sm.serial,
				Sort:           i + 1,
				FinanceEnabled: true,
			}
			if err := s.SaveMeter(&m); err != nil {
				return err
			}
			if oi == 0 && i == 0 {
				firstCold = m.ID
			}
			val := sm.base
			for p := def.from; !p.After(end); p = p.Next() {
				val = model.Round3(val + sm.step)
				if err := s.SaveReading(m.ID, p.String(), val, ""); err != nil {
					return err
				}
			}
		}

		// Суб-счётчик холодной воды у первого объекта.
		if oi == 0 && firstCold != 0 {
			child := model.Meter{
				ObjectID: obj.ID,
				Resource: model.WaterCold,
				Label:    "Полив, суб-счётчик",
				Brand:    "СВК-15",
				Serial:   "118499",
				ParentID: firstCold,
				Sort:     4,
			}
			if err := s.SaveMeter(&child); err != nil {
				return err
			}
			val := 36.0
			for p := def.from; !p.After(end); p = p.Next() {
				val = model.Round3(val + 1.1)
				if err := s.SaveReading(child.ID, p.String(), val, ""); err != nil {
					return err
				}
			}
		}

		// Начисления и оплаты.
		for p := def.from; !p.After(end); p = p.Next() {
			idx := float64(def.from.MonthDiff(p))
			waterCharged := model.Round2(900 + idx*25)
			energyCharged := model.Round2(2400 + idx*35)
			energyPaid := energyCharged
			if p.Month%3 == 0 {
				energyPaid = model.Round2(energyCharged - 610)
			}
			coldCharged := model.Round2(waterCharged * 0.6)
			hotCharged := model.Round2(waterCharged - coldCharged)
			items := []model.Money{
				{ObjectID: obj.ID, Period: p.String(), Account: model.AccountWaterCold, Charged: coldCharged, Paid: coldCharged, PaidFlag: true},
				{ObjectID: obj.ID, Period: p.String(), Account: model.AccountWaterHot, Charged: hotCharged, Paid: hotCharged, PaidFlag: true},
				{ObjectID: obj.ID, Period: p.String(), Account: model.AccountEnergy, Charged: energyCharged, Paid: energyPaid},
			}
			if err := s.SaveMoney(items, p.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

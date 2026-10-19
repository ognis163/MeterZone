package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"indextro/internal/model"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"), time.Local)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestObjectsMetersReadings(t *testing.T) {
	st := newStore(t)

	obj := model.Object{Name: "Дом", Kind: model.KindHouse, Address: "ул. Тестовая, 1", Owner: "Иванов"}
	if err := st.SaveObject(&obj); err != nil {
		t.Fatalf("SaveObject: %v", err)
	}
	if obj.ID == 0 {
		t.Fatal("не заполнен ID объекта")
	}

	m1 := model.Meter{ObjectID: obj.ID, Resource: model.WaterCold, Label: "Кухня", StartValue: 100, Sort: 2}
	m2 := model.Meter{ObjectID: obj.ID, Resource: model.Energy, Label: "Щиток", Sort: 1}
	if err := st.SaveMeter(&m1); err != nil {
		t.Fatalf("SaveMeter: %v", err)
	}
	if err := st.SaveMeter(&m2); err != nil {
		t.Fatalf("SaveMeter: %v", err)
	}

	meters, err := st.Meters(obj.ID, false)
	if err != nil {
		t.Fatalf("Meters: %v", err)
	}
	if len(meters) != 2 {
		t.Fatalf("ожидалось 2 прибора, получено %d", len(meters))
	}
	// Порядок: сначала вода, затем электричество.
	if meters[0].Resource != model.WaterCold || meters[1].Resource != model.Energy {
		t.Fatalf("неверный порядок: %v, %v", meters[0].Resource, meters[1].Resource)
	}

	if err := st.SaveReadings([]ReadingInput{
		{MeterID: m1.ID, Value: 110},
		{MeterID: m2.ID, Value: 2050},
	}, "2026-10"); err != nil {
		t.Fatalf("SaveReadings: %v", err)
	}
	// Перезапись того же месяца.
	if err := st.SaveReading(m1.ID, "2026-10", 111, "уточнение"); err != nil {
		t.Fatalf("SaveReading: %v", err)
	}

	got, err := st.ReadingsForMonth(obj.ID, "2026-10")
	if err != nil {
		t.Fatalf("ReadingsForMonth: %v", err)
	}
	if len(got) != 2 || got[m1.ID].Value != 111 {
		t.Fatalf("неверные показания: %+v", got)
	}

	all, err := st.ReadingsForObject(obj.ID)
	if err != nil {
		t.Fatalf("ReadingsForObject: %v", err)
	}
	if all[m1.ID]["2026-10"] != 111 {
		t.Fatalf("чтение по объекту: %+v", all)
	}

	deleted, err := st.DeleteMonth(obj.ID, "2026-10")
	if err != nil || deleted != 2 {
		t.Fatalf("DeleteMonth: %d, %v", deleted, err)
	}
}

func TestDeleteObjectCascades(t *testing.T) {
	st := newStore(t)
	obj := model.Object{Name: "Коммерция", Kind: model.KindCommercial}
	_ = st.SaveObject(&obj)
	m := model.Meter{ObjectID: obj.ID, Resource: model.WaterHot}
	_ = st.SaveMeter(&m)
	_ = st.SaveReading(m.ID, "2026-01", 10, "")
	if err := st.DeleteObject(obj.ID); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := st.Object(obj.ID); err == nil {
		t.Fatal("объект не удалён")
	}
	all, _ := st.ReadingsForObject(obj.ID)
	if len(all) != 0 {
		t.Fatalf("остались показания: %+v", all)
	}
}

func TestBackups(t *testing.T) {
	st := newStore(t)
	obj := model.Object{Name: "Дом"}
	_ = st.SaveObject(&obj)
	dir := filepath.Join(t.TempDir(), "backups")

	for i := 0; i < 4; i++ {
		if _, err := st.Backup(dir); err != nil {
			t.Fatalf("Backup: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	list, err := st.ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("копий %d, ожидалось 4", len(list))
	}
	if err := ValidateDBFile(list[0].Path); err != nil {
		t.Fatalf("ValidateDBFile: %v", err)
	}
	removed, err := st.PruneBackups(dir, 2)
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("удалено %d, ожидалось 2", len(removed))
	}
	list, _ = st.ListBackups(dir)
	if len(list) != 2 {
		t.Fatalf("после чистки %d копий", len(list))
	}

	// Копия должна открываться как база и содержать объект.
	st2, err := Open(list[0].Path, time.Local)
	if err != nil {
		t.Fatalf("открытие копии: %v", err)
	}
	defer st2.Close()
	if objs, _ := st2.Objects(true); len(objs) != 1 {
		t.Fatalf("в копии %d объектов", len(objs))
	}
}

func TestImportLegacy(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.db")
	old, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatalf("open old: %v", err)
	}
	schema := []string{
		`CREATE TABLE addresses (id INTEGER PRIMARY KEY, name TEXT, org_name TEXT, address TEXT, subscriber TEXT)`,
		`CREATE TABLE meters (id INTEGER PRIMARY KEY, address_id INTEGER, type TEXT, location TEXT, brand TEXT, serial TEXT, commercial INTEGER)`,
		`CREATE TABLE readings (id INTEGER PRIMARY KEY, meter_id INTEGER, year INTEGER, month_num INTEGER, prev REAL, curr REAL, filled INTEGER)`,
		`INSERT INTO addresses (id,name,org_name,address,subscriber) VALUES (1,'Дом','','ул. Старая, 2','12345')`,
		`INSERT INTO meters (id,address_id,type,location,brand,serial,commercial) VALUES (10,1,'Холодная','Кухня','СВК-15','A1',0)`,
		`INSERT INTO meters (id,address_id,type,location,brand,serial,commercial) VALUES (11,1,'Электроэнергия','Щиток','Меркурий','B2',1)`,
		`INSERT INTO readings (meter_id,year,month_num,prev,curr,filled) VALUES (10,2026,9,100,110,1)`,
		`INSERT INTO readings (meter_id,year,month_num,prev,curr,filled) VALUES (10,2026,10,110,121,1)`,
		`INSERT INTO readings (meter_id,year,month_num,prev,curr,filled) VALUES (11,2026,10,1000,1050,1)`,
		`INSERT INTO readings (meter_id,year,month_num,prev,curr,filled) VALUES (10,2026,11,0,0,0)`,
	}
	for _, q := range schema {
		if _, err := old.Exec(q); err != nil {
			t.Fatalf("схема старой базы: %v", err)
		}
	}
	old.Close()

	st := newStore(t)
	stats, err := st.ImportLegacy(oldPath)
	if err != nil {
		t.Fatalf("ImportLegacy: %v", err)
	}
	if stats.Objects != 1 || stats.Meters != 2 || stats.Readings != 3 {
		t.Fatalf("статистика импорта: %+v", stats)
	}
	objs, _ := st.Objects(false)
	if len(objs) != 1 || objs[0].Kind != model.KindCommercial {
		t.Fatalf("объект импортирован неверно: %+v", objs)
	}
	if objs[0].AccountWater != "12345" || objs[0].AccountEnergy != "12345" {
		t.Fatalf("лицевые счета: %+v", objs[0])
	}
	meters, _ := st.Meters(objs[0].ID, true)
	if len(meters) != 2 {
		t.Fatalf("приборов %d", len(meters))
	}
	readings, _ := st.ReadingsForObject(objs[0].ID)
	total := 0
	for _, series := range readings {
		total += len(series)
	}
	if total != 3 {
		t.Fatalf("показаний %d, ожидалось 3", total)
	}
}

func TestImportLegacyRejectsForeignDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foreign.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE foo (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st := newStore(t)
	if _, err := st.ImportLegacy(path); err == nil {
		t.Fatal("ожидалась ошибка для чужой базы")
	}
}

func TestSettingsAndStats(t *testing.T) {
	st := newStore(t)
	_ = st.SetSetting("key", "value")
	if got := st.Setting("key", ""); got != "value" {
		t.Fatalf("Setting = %q", got)
	}
	if got := st.Setting("missing", "def"); got != "def" {
		t.Fatalf("Setting default = %q", got)
	}
	stats := st.Stats()
	if stats.Objects != 0 || stats.Readings != 0 {
		t.Fatalf("Stats = %+v", stats)
	}
}

func TestRestoreFrom(t *testing.T) {
	st := newStore(t)
	obj := model.Object{Name: "Исходный"}
	if err := st.SaveObject(&obj); err != nil {
		t.Fatal(err)
	}
	m := model.Meter{ObjectID: obj.ID, Resource: model.WaterCold}
	_ = st.SaveMeter(&m)
	_ = st.SaveReading(m.ID, "2026-01", 5, "")

	dir := filepath.Join(t.TempDir(), "backups")
	b, err := st.Backup(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Портим данные после копии.
	other := model.Object{Name: "Лишний"}
	_ = st.SaveObject(&other)
	_ = st.DeleteObject(obj.ID)

	stats, err := st.RestoreFrom(b.Path, dir)
	if err != nil {
		t.Fatalf("RestoreFrom: %v", err)
	}
	if stats.Objects != 1 || stats.Meters != 1 || stats.Readings != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	objs, _ := st.Objects(true)
	if len(objs) != 1 || objs[0].Name != "Исходный" {
		t.Fatalf("после восстановления: %+v", objs)
	}
}

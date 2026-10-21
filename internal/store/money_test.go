package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"indextro/internal/model"
)

func TestMoneyCRUD(t *testing.T) {
	st := newStore(t)
	obj := model.Object{Name: "Дом"}
	if err := st.SaveObject(&obj); err != nil {
		t.Fatal(err)
	}
	items := []model.Money{
		{ObjectID: obj.ID, Period: "2026-10", Account: model.AccountWaterCold, Charged: 1200.50, Paid: 1000, AdvanceSum: 1000, AdvancePaid: true},
		{ObjectID: obj.ID, Period: "2026-10", Account: model.AccountEnergy, Charged: 2400, Paid: 2400},
	}
	if err := st.SaveMoney(items, "2026-10"); err != nil {
		t.Fatalf("SaveMoney: %v", err)
	}
	got, err := st.MoneyForMonth(obj.ID, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if got[model.AccountWaterCold].Charged != 1200.5 || got[model.AccountWaterCold].Debt() != 200.5 {
		t.Fatalf("вода: %+v", got[model.AccountWaterCold])
	}
	if got[model.AccountEnergy].Debt() != 0 {
		t.Fatalf("свет: %+v", got[model.AccountEnergy])
	}

	// Нулевые значения удаляют запись.
	if err := st.SaveMoney([]model.Money{
		{ObjectID: obj.ID, Period: "2026-10", Account: model.AccountWaterCold, Charged: 0, Paid: 0},
	}, "2026-10"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.MoneyForMonth(obj.ID, "2026-10")
	if _, ok := got[model.AccountWaterCold]; ok {
		t.Fatal("нулевое начисление не удалено")
	}

	if err := st.SaveMoney([]model.Money{
		{ObjectID: obj.ID, Period: "2026-09", Account: model.AccountWaterCold, Charged: 1000, Paid: 500},
	}, "2026-09"); err != nil {
		t.Fatal(err)
	}
	totals, err := st.MoneyTotals(obj.ID, model.Period{Year: 2026, Month: 9}, model.Period{Year: 2026, Month: 10})
	if err != nil {
		t.Fatal(err)
	}
	if totals[model.AccountWaterCold].Charged != 1000 || totals[model.AccountWaterCold].Paid != 500 {
		t.Fatalf("итоги: %+v", totals[model.AccountWaterCold])
	}
}

func TestMigrationMoneyAdvance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE money (
			object_id INTEGER NOT NULL, period TEXT NOT NULL, account TEXT NOT NULL,
			charged REAL NOT NULL DEFAULT 0, paid REAL NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
			PRIMARY KEY (object_id, period, account))`,
		`INSERT INTO money VALUES (1,'2026-10','water',1200,1000,'','2026-10-01T00:00:00Z')`,
		`INSERT INTO money VALUES (1,'2026-10','energy',2400,2400,'','2026-10-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	st, err := Open(path, time.Local)
	if err != nil {
		t.Fatalf("Open v3: %v", err)
	}
	defer st.Close()
	got, err := st.MoneyForMonth(1, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	water := got[model.AccountWaterCold]
	if water.Debt() != 200 || water.AdvanceSum != 1000 || water.PaidFlag {
		t.Fatalf("вода после миграции: %+v", water)
	}
	energy := got[model.AccountEnergy]
	if energy.Debt() != 0 || energy.AdvanceSum != 0 || !energy.PaidFlag {
		t.Fatalf("электро после миграции: %+v", energy)
	}
}

// TestMigrationClearsPaidOnEmptyMoney — нулевая сумма не может быть
// оплачена: старые отметки на пустых позициях снимаются при открытии.
func TestMigrationClearsPaidOnEmptyMoney(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	st, err := Open(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	obj := model.Object{Name: "Дом"}
	if err := st.SaveObject(&obj); err != nil {
		t.Fatal(err)
	}
	// Такую запись оставляла прежняя отметка «Итого»: пустая позиция оплачена.
	if _, err := st.DB().Exec(`INSERT INTO money (object_id, period, account, charged, paid, advance, advance_paid, paid_flag, note, updated_at)
		VALUES (?, '2026-10', 'energy', 0, 0, 0, 1, 1, '', '2026-10-01T00:00:00Z')`, obj.ID); err != nil {
		t.Fatal(err)
	}
	st.Close()

	again, err := Open(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, err := again.MoneyForMonth(obj.ID, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	m := got[model.AccountEnergy]
	if m.PaidFlag || m.AdvancePaid {
		t.Fatalf("оплата пустой позиции осталась: %+v", m)
	}
}

func TestAvailableMonths(t *testing.T) {
	st := newStore(t)
	obj := model.Object{Name: "Дом"}
	_ = st.SaveObject(&obj)
	m := model.Meter{ObjectID: obj.ID, Resource: model.WaterCold}
	_ = st.SaveMeter(&m)
	_ = st.SaveReading(m.ID, "2026-01", 1, "")
	_ = st.SaveReading(m.ID, "2026-03", 2, "")

	months, err := st.AvailableMonths(obj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 2 || months[0].String() != "2026-01" || months[1].String() != "2026-03" {
		t.Fatalf("месяцы: %v", months)
	}
	if !st.HasReadings(obj.ID, "2026-03") || st.HasReadings(obj.ID, "2026-02") {
		t.Fatal("HasReadings")
	}
}

func TestMigrationFromV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE objects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, kind TEXT NOT NULL,
			address TEXT NOT NULL, owner TEXT NOT NULL, account_water TEXT NOT NULL, account_energy TEXT NOT NULL,
			note TEXT NOT NULL, archived INTEGER NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE meters (id INTEGER PRIMARY KEY AUTOINCREMENT, object_id INTEGER NOT NULL, resource TEXT NOT NULL,
			label TEXT NOT NULL, brand TEXT NOT NULL, serial TEXT NOT NULL, start_value REAL NOT NULL,
			installed_on TEXT NOT NULL, archived INTEGER NOT NULL, sort INTEGER NOT NULL)`,
		`CREATE TABLE readings (meter_id INTEGER NOT NULL, period TEXT NOT NULL, value REAL NOT NULL,
			note TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(meter_id, period))`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO objects (id,name,kind,address,owner,account_water,account_energy,note,archived,created_at)
			VALUES (1,'Старый дом','house','ул. Старая, 1','Иванов','1','2','',0,'2026-01-01T00:00:00Z')`,
		`INSERT INTO meters (id,object_id,resource,label,brand,serial,start_value,installed_on,archived,sort)
			VALUES (1,1,'water_cold','Кухня','СВК','1',0,'',0,0)`,
		`INSERT INTO readings (meter_id,period,value,note,updated_at) VALUES (1,'2026-01',10,'','2026-01-31T00:00:00Z')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("схема v1: %v", err)
		}
	}
	db.Close()

	st, err := Open(path, time.Local)
	if err != nil {
		t.Fatalf("Open v1: %v", err)
	}
	defer st.Close()

	obj, err := st.Object(1)
	if err != nil {
		t.Fatal(err)
	}
	if obj.EmailWater != "" || obj.EmailEnergy != "" {
		t.Fatalf("почта должна быть пустой: %+v", obj)
	}
	m, err := st.Meter(1)
	if err != nil {
		t.Fatal(err)
	}
	if m.ParentID != 0 {
		t.Fatalf("parent_id = %d", m.ParentID)
	}
	obj.EmailWater = "water@example.ru"
	if err := st.SaveObject(&obj); err != nil {
		t.Fatal(err)
	}
	m.ParentID = 0
	if err := st.SaveMeter(&m); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MoneyForMonth(1, "2026-01"); err != nil {
		t.Fatalf("таблица money не создана: %v", err)
	}
}

func TestRestoreFromV1Backup(t *testing.T) {
	st := newStore(t)
	obj := model.Object{Name: "Текущий"}
	_ = st.SaveObject(&obj)

	// Готовим копию старого формата без новых полей.
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old-backup.db")
	db, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE objects (id INTEGER PRIMARY KEY, name TEXT, kind TEXT, address TEXT, owner TEXT,
			account_water TEXT, account_energy TEXT, note TEXT, archived INTEGER, created_at TEXT)`,
		`CREATE TABLE meters (id INTEGER PRIMARY KEY, object_id INTEGER, resource TEXT, label TEXT, brand TEXT,
			serial TEXT, start_value REAL, installed_on TEXT, archived INTEGER, sort INTEGER)`,
		`CREATE TABLE readings (meter_id INTEGER, period TEXT, value REAL, note TEXT, updated_at TEXT,
			PRIMARY KEY(meter_id, period))`,
		`INSERT INTO objects VALUES (1,'Из копии','house','','','','','',0,'2026-01-01T00:00:00Z')`,
		`INSERT INTO meters VALUES (1,1,'energy','Щиток','','',0,'',0,0)`,
		`INSERT INTO readings VALUES (1,'2026-01',5,'','2026-01-31T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	stats, err := st.RestoreFrom(oldPath, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatalf("RestoreFrom v1: %v", err)
	}
	if stats.Objects != 1 || stats.Meters != 1 || stats.Readings != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	objs, _ := st.Objects(true)
	if len(objs) != 1 || objs[0].Name != "Из копии" {
		t.Fatalf("объекты после восстановления: %+v", objs)
	}
}

package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"indextro/internal/model"
)

func TestImportLegacyMoney(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE addresses (id INTEGER PRIMARY KEY, name TEXT, org_name TEXT, address TEXT,
			subscriber TEXT, subscriber_water TEXT, subscriber_energy TEXT)`,
		`CREATE TABLE meters (id INTEGER PRIMARY KEY, address_id INTEGER, type TEXT, location TEXT,
			brand TEXT, serial TEXT, commercial INTEGER, is_active INTEGER)`,
		`CREATE TABLE readings (id INTEGER PRIMARY KEY, meter_id INTEGER, year INTEGER, month_num INTEGER,
			prev REAL, curr REAL, advance REAL, bill REAL, filled INTEGER)`,
		`INSERT INTO addresses VALUES (1,'Дом','','','','','')`,
		`INSERT INTO meters VALUES (1,1,'Холодная','Кухня','','',0,1)`,
		`INSERT INTO meters VALUES (2,1,'Электроэнергия','Щиток','','',0,1)`,
		`INSERT INTO readings (meter_id,year,month_num,prev,curr,advance,bill,filled)
			VALUES (1,2026,1,10,12,500,900,1)`,
		`INSERT INTO readings (meter_id,year,month_num,prev,curr,advance,bill,filled)
			VALUES (2,2026,1,100,120,2000,2500,1)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	db.Close()

	st := newStore(t)
	stats, err := st.ImportLegacy(path)
	if err != nil {
		t.Fatalf("ImportLegacy: %v", err)
	}
	if stats.Money != 2 {
		t.Fatalf("начислений %d, ожидалось 2", stats.Money)
	}
	objs, _ := st.Objects(false)
	money, err := st.MoneyForMonth(objs[0].ID, "2026-01")
	if err != nil {
		t.Fatal(err)
	}
	if money[model.AccountWaterCold].Charged != 900 || money[model.AccountWaterCold].Paid != 500 {
		t.Fatalf("вода: %+v", money[model.AccountWaterCold])
	}
	if money[model.AccountEnergy].Charged != 2500 || money[model.AccountEnergy].Paid != 2000 {
		t.Fatalf("свет: %+v", money[model.AccountEnergy])
	}
}

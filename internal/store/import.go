package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"indextro/internal/model"
)

// ImportStats — итоги импорта старой базы.
type ImportStats struct {
	Objects  int
	Meters   int
	Readings int
	Money    int
	Skipped  int
}

// ImportLegacy импортирует данные приложения MeterZone 0.x
// (таблицы addresses, meters, readings) как новые объекты.
func (s *Store) ImportLegacy(path string) (ImportStats, error) {
	var stats ImportStats
	if err := ValidateDBFile(path); err != nil {
		return stats, err
	}

	old, err := sql.Open("sqlite", path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return stats, fmt.Errorf("открытие старой базы: %w", err)
	}
	defer old.Close()
	old.SetMaxOpenConns(1)

	tables, err := tableSet(old)
	if err != nil {
		return stats, err
	}
	for _, need := range []string{"addresses", "meters", "readings"} {
		if !tables[need] {
			return stats, fmt.Errorf("в файле нет таблицы %q — это не база MeterZone", need)
		}
	}

	addrCols, _ := tableColumns(old, "addresses")
	meterCols, _ := tableColumns(old, "meters")
	readCols, _ := tableColumns(old, "readings")

	selAddr := strings.Join([]string{
		"id",
		colExpr(addrCols, "name", ""),
		colExpr(addrCols, "org_name", ""),
		colExpr(addrCols, "address", ""),
		colExpr(addrCols, "subscriber", ""),
		colExpr(addrCols, "subscriber_water", ""),
		colExpr(addrCols, "subscriber_energy", ""),
		colExpr(addrCols, "email_water", ""),
		colExpr(addrCols, "email_energy", ""),
	}, ", ")

	rows, err := old.Query("SELECT " + selAddr + " FROM addresses ORDER BY id")
	if err != nil {
		return stats, fmt.Errorf("чтение объектов: %w", err)
	}

	type legacyAddr struct {
		id                             int64
		name, org, address, subscriber string
		subWater, subEnergy            string
		emailWater, emailEnergy        string
	}
	var addrs []legacyAddr
	for rows.Next() {
		var a legacyAddr
		if err := rows.Scan(&a.id, &a.name, &a.org, &a.address, &a.subscriber,
			&a.subWater, &a.subEnergy, &a.emailWater, &a.emailEnergy); err != nil {
			rows.Close()
			return stats, err
		}
		addrs = append(addrs, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}
	if len(addrs) == 0 {
		return stats, fmt.Errorf("в старой базе нет объектов")
	}

	// Приборы.
	selMeter := strings.Join([]string{
		"id", "address_id",
		colExpr(meterCols, "type", ""),
		colExpr(meterCols, "location", ""),
		colExpr(meterCols, "brand", ""),
		colExpr(meterCols, "serial", ""),
		colExpr(meterCols, "commercial", "0"),
		colExpr(meterCols, "is_active", "1"),
	}, ", ")
	meterRows, err := old.Query("SELECT " + selMeter + " FROM meters ORDER BY id")
	if err != nil {
		return stats, fmt.Errorf("чтение приборов: %w", err)
	}
	type legacyMeter struct {
		id, addrID           int64
		typ, label, brand    string
		serial               string
		commercial, isActive int
	}
	var meters []legacyMeter
	for meterRows.Next() {
		var m legacyMeter
		if err := meterRows.Scan(&m.id, &m.addrID, &m.typ, &m.label, &m.brand, &m.serial, &m.commercial, &m.isActive); err != nil {
			meterRows.Close()
			return stats, err
		}
		meters = append(meters, m)
	}
	meterRows.Close()
	if err := meterRows.Err(); err != nil {
		return stats, err
	}

	// Показания.
	selRead := strings.Join([]string{
		"meter_id", "year", "month_num",
		colExpr(readCols, "curr", "0"),
		colExpr(readCols, "filled", "1"),
		colExpr(readCols, "advance", "0"),
		colExpr(readCols, "bill", "0"),
	}, ", ")
	readRows, err := old.Query("SELECT " + selRead + " FROM readings ORDER BY year, month_num")
	if err != nil {
		return stats, fmt.Errorf("чтение показаний: %w", err)
	}
	type legacyReading struct {
		meterID     int64
		year, month int
		curr        float64
		filled      int
		advance     float64
		bill        float64
	}
	var readings []legacyReading
	for readRows.Next() {
		var r legacyReading
		if err := readRows.Scan(&r.meterID, &r.year, &r.month, &r.curr, &r.filled, &r.advance, &r.bill); err != nil {
			readRows.Close()
			return stats, err
		}
		readings = append(readings, r)
	}
	readRows.Close()
	if err := readRows.Err(); err != nil {
		return stats, err
	}

	commercialByAddr := map[int64]bool{}
	for _, m := range meters {
		if m.commercial != 0 {
			commercialByAddr[m.addrID] = true
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()

	now := time.Now().In(s.loc).Format(time.RFC3339)
	objMap := map[int64]int64{}
	for _, a := range addrs {
		owner := strings.TrimSpace(a.org)
		if owner == "" {
			owner = strings.TrimSpace(a.subscriber)
		}
		water := strings.TrimSpace(a.subWater)
		if water == "" {
			water = strings.TrimSpace(a.subscriber)
		}
		energy := strings.TrimSpace(a.subEnergy)
		if energy == "" {
			energy = strings.TrimSpace(a.subscriber)
		}
		kind := model.KindHouse
		if commercialByAddr[a.id] {
			kind = model.KindCommercial
		}
		res, err := tx.Exec(`INSERT INTO objects (name, kind, address, owner, account_water, account_energy,
				email_water, email_energy, note, archived, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', 0, ?)`,
			strings.TrimSpace(a.name), kind, strings.TrimSpace(a.address), owner, water, energy,
			strings.TrimSpace(a.emailWater), strings.TrimSpace(a.emailEnergy), now)
		if err != nil {
			return stats, err
		}
		newID, _ := res.LastInsertId()
		objMap[a.id] = newID
		stats.Objects++
	}

	meterMap := map[int64]int64{}
	for _, m := range meters {
		objID, ok := objMap[m.addrID]
		if !ok {
			stats.Skipped++
			continue
		}
		archived := m.isActive == 0
		res, err := tx.Exec(`INSERT INTO meters (object_id, resource, label, brand, serial, start_value, installed_on, parent_id, archived, sort)
			VALUES (?, ?, ?, ?, ?, 0, '', 0, ?, 0)`,
			objID, legacyResource(m.typ), strings.TrimSpace(m.label), strings.TrimSpace(m.brand),
			strings.TrimSpace(m.serial), boolInt(archived))
		if err != nil {
			return stats, err
		}
		newID, _ := res.LastInsertId()
		meterMap[m.id] = newID
		stats.Meters++
	}

	type moneyKey struct {
		objID   int64
		period  string
		account model.Account
	}
	moneyTotals := map[moneyKey]*model.Money{}
	meterByID := map[int64]legacyMeter{}
	for _, m := range meters {
		meterByID[m.id] = m
	}

	for _, r := range readings {
		newID, ok := meterMap[r.meterID]
		if !ok {
			stats.Skipped++
			continue
		}
		p, err := model.NewPeriod(r.year, r.month)
		if err != nil {
			stats.Skipped++
			continue
		}
		lm := meterByID[r.meterID]
		if objID, ok := objMap[lm.addrID]; ok && (r.advance != 0 || r.bill != 0) {
			key := moneyKey{objID: objID, period: p.String(), account: model.AccountFor(legacyResource(lm.typ))}
			mt := moneyTotals[key]
			if mt == nil {
				mt = &model.Money{ObjectID: objID, Period: p.String(), Account: key.account, Note: "Импорт старой базы"}
				moneyTotals[key] = mt
			}
			mt.Charged = model.Round2(mt.Charged + r.bill)
			mt.AdvanceSum = model.Round2(mt.AdvanceSum + r.advance)
			mt.AdvancePaid = mt.AdvanceSum > 0
			mt.Paid = mt.AdvanceSum
		}
		if r.filled == 0 && r.curr == 0 {
			stats.Skipped++
			continue
		}
		if _, err := tx.Exec(`INSERT INTO readings (meter_id, period, value, note, updated_at)
			VALUES (?, ?, ?, '', ?)
			ON CONFLICT(meter_id, period) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			newID, p.String(), r.curr, now); err != nil {
			return stats, err
		}
		stats.Readings++
	}

	for _, m := range moneyTotals {
		if _, err := tx.Exec(`INSERT INTO money (object_id, period, account, charged, paid, advance, advance_paid, paid_flag, note, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
			ON CONFLICT(object_id, period, account) DO UPDATE SET
				charged = excluded.charged, paid = excluded.paid, advance = excluded.advance,
				advance_paid = excluded.advance_paid,
				paid_flag = excluded.paid_flag, note = excluded.note, updated_at = excluded.updated_at`,
			m.ObjectID, m.Period, m.Account, m.Charged, m.Paid, m.AdvanceSum, boolInt(m.AdvancePaid), m.Note, now); err != nil {
			return stats, err
		}
		stats.Money++
	}

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

func legacyResource(t string) model.Resource {
	s := strings.ToLower(strings.TrimSpace(t))
	switch {
	case strings.Contains(s, "горяч") || s == "гвс":
		return model.WaterHot
	case strings.Contains(s, "холод") || s == "хвс":
		return model.WaterCold
	case strings.Contains(s, "электр") || strings.Contains(s, "свет"):
		return model.Energy
	}
	return model.WaterCold
}

func tableSet(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = true
	}
	return out, rows.Err()
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info('%s')`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = true
	}
	return out, rows.Err()
}

func colExpr(cols map[string]bool, name, def string) string {
	if def == "" {
		def = "''"
	}
	if cols[strings.ToLower(name)] {
		return "COALESCE(" + name + ", " + def + ")"
	}
	return def
}

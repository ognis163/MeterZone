package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// RestoreStats — итоги восстановления базы.
type RestoreStats struct {
	Objects  int
	Meters   int
	Readings int
	Money    int
}

// RestoreFrom заменяет текущие данные содержимым другой базы MeterZone.
// Понимает и старые копии без полей второй версии.
// Перед заменой автоматически создаётся резервная копия.
// Операция выполняется одной транзакцией: при ошибке данные не меняются.
func (s *Store) RestoreFrom(path, backupDir string) (RestoreStats, error) {
	var stats RestoreStats
	if err := ValidateDBFile(path); err != nil {
		return stats, err
	}
	if _, err := s.Backup(backupDir); err != nil {
		return stats, fmt.Errorf("не удалось сделать копию перед восстановлением: %w", err)
	}

	attachPath := strings.ReplaceAll(path, "'", "''")
	if _, err := s.db.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS restore", attachPath)); err != nil {
		return stats, fmt.Errorf("не удалось открыть файл копии: %w", err)
	}
	defer func() {
		_, _ = s.db.Exec("DETACH DATABASE restore")
	}()

	required := map[string][]string{
		"objects":  {"id", "name", "kind", "address", "owner", "account_water", "account_energy", "note", "archived", "created_at"},
		"meters":   {"id", "object_id", "resource", "label", "brand", "serial", "start_value", "installed_on", "archived", "sort"},
		"readings": {"meter_id", "period", "value", "note", "updated_at"},
	}
	columns := map[string]map[string]bool{}
	for table, cols := range required {
		have, err := attachedColumns(s.db, table)
		if err != nil {
			return stats, fmt.Errorf("не удалось прочитать структуру копии: %w", err)
		}
		columns[table] = have
		for _, c := range cols {
			if !have[c] {
				return stats, fmt.Errorf("файл не похож на базу MeterZone: в таблице %s нет поля %s", table, c)
			}
		}
	}

	opt := func(table, col, def string) string {
		if columns[table][col] {
			return "COALESCE(" + col + ", " + def + ")"
		}
		return def
	}
	emailW := opt("objects", "email_water", "''")
	emailE := opt("objects", "email_energy", "''")
	finance := opt("objects", "finance_enabled", "0")
	parentID := opt("meters", "parent_id", "0")

	hasMoney := false
	if tables, err := attachedTables(s.db); err == nil {
		hasMoney = tables["money"]
	}
	if hasMoney {
		columns["money"], _ = attachedColumns(s.db, "money")
	}
	advance := opt("money", "advance", "CASE WHEN paid >= charged THEN paid - charged ELSE paid END")
	paidFlag := opt("money", "paid_flag", "CASE WHEN charged > 0 AND paid >= charged THEN 1 ELSE 0 END")
	advancePaid := opt("money", "advance_paid", "CASE WHEN advance > 0 THEN 1 ELSE 0 END")
	meterFinance := opt("meters", "finance_enabled", "1")

	tx, err := s.db.Begin()
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()

	stmts := []string{
		`DELETE FROM money`,
		`DELETE FROM readings`,
		`DELETE FROM meters`,
		`DELETE FROM objects`,
		`INSERT INTO objects (id, name, kind, address, owner, account_water, account_energy,
			email_water, email_energy, finance_enabled, note, archived, created_at)
			SELECT id, name, kind, address, owner, account_water, account_energy,
			` + emailW + `, ` + emailE + `, ` + finance + `, note, archived, created_at FROM restore.objects`,
		`INSERT INTO meters (id, object_id, resource, label, brand, serial, start_value,
			installed_on, parent_id, finance_enabled, archived, sort)
			SELECT id, object_id, resource, label, brand, serial, start_value,
			installed_on, ` + parentID + `, ` + meterFinance + `, archived, sort FROM restore.meters`,
		`INSERT INTO readings (meter_id, period, value, note, updated_at)
			SELECT meter_id, period, value, note, updated_at FROM restore.readings`,
	}
	if hasMoney {
		stmts = append(stmts, `INSERT INTO money (object_id, period, account, charged, paid, advance, advance_paid, paid_flag, note, updated_at)
			SELECT object_id, period, CASE WHEN account = 'water' THEN 'water_cold' ELSE account END,
			charged, paid, `+advance+`, `+advancePaid+`, `+paidFlag+`, note, updated_at FROM restore.money`)
		if !columns["objects"]["finance_enabled"] {
			stmts = append(stmts, `UPDATE objects SET finance_enabled = 1 WHERE id IN (SELECT DISTINCT object_id FROM money)`)
		}
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			return stats, fmt.Errorf("восстановление: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return stats, err
	}

	_ = s.db.QueryRow(`SELECT COUNT(*) FROM objects`).Scan(&stats.Objects)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM meters`).Scan(&stats.Meters)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&stats.Readings)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM money`).Scan(&stats.Money)
	return stats, nil
}

func attachedColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA restore.table_info('%s')`, table))
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

func attachedTables(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM restore.sqlite_master WHERE type = 'table'`)
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

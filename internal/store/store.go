// Package store — хранилище данных приложения на SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"indextro/internal/model"
)

// Store — обёртка над SQLite с одним соединением (роутер, мало памяти).
type Store struct {
	db   *sql.DB
	path string
	loc  *time.Location
}

// ErrNotFound возвращается, когда запись не найдена.
var ErrNotFound = errors.New("запись не найдена")

const schemaVersion = 5

// Open открывает базу, применяет схему и проверяет целостность.
func Open(path string, loc *time.Location) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("путь к базе не задан")
	}
	if loc == nil {
		loc = time.Local
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("каталог базы: %w", err)
	}

	dsn := path + "?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=temp_store(MEMORY)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("открытие базы: %w", err)
	}
	// Одно соединение: предсказуемая память и отсутствие блокировок на роутере.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, path: path, loc: loc}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("подключение к базе: %w", err)
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.quickCheck(); err != nil {
		// Не фатально: база может быть исправлена backup/restore, но сообщаем.
		fmt.Fprintf(os.Stderr, "предупреждение: проверка целостности базы: %v\n", err)
	}
	return s, nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

// Path возвращает путь к файлу базы.
func (s *Store) Path() string { return s.path }

// DB даёт доступ к *sql.DB (только для внутренних задач, например бэкапа).
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS objects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'house',
			address TEXT NOT NULL DEFAULT '',
			owner TEXT NOT NULL DEFAULT '',
			account_water TEXT NOT NULL DEFAULT '',
			account_energy TEXT NOT NULL DEFAULT '',
			email_water TEXT NOT NULL DEFAULT '',
			email_energy TEXT NOT NULL DEFAULT '',
			finance_enabled INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS meters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			object_id INTEGER NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			resource TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			brand TEXT NOT NULL DEFAULT '',
			serial TEXT NOT NULL DEFAULT '',
			start_value REAL NOT NULL DEFAULT 0,
			installed_on TEXT NOT NULL DEFAULT '',
			parent_id INTEGER NOT NULL DEFAULT 0,
			finance_enabled INTEGER NOT NULL DEFAULT 0,
			finance_mode TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			sort INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_meters_object ON meters(object_id, archived, sort);`,
		`CREATE TABLE IF NOT EXISTS readings (
			meter_id INTEGER NOT NULL REFERENCES meters(id) ON DELETE CASCADE,
			period TEXT NOT NULL,
			value REAL NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (meter_id, period)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_readings_period ON readings(period);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS money (
			object_id INTEGER NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			period TEXT NOT NULL,
			account TEXT NOT NULL,
			charged REAL NOT NULL DEFAULT 0,
			paid REAL NOT NULL DEFAULT 0,
			advance REAL NOT NULL DEFAULT 0,
			advance_paid INTEGER NOT NULL DEFAULT 0,
			paid_flag INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (object_id, period, account)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_money_period ON money(period);`,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("миграция: %w", err)
	}
	defer tx.Rollback()
	for _, q := range schema {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("миграция: %w", err)
		}
	}
	// Колонки, добавленные во второй версии. Повторный ALTER безвреден:
	// ошибка «duplicate column» просто игнорируется.
	for _, q := range []string{
		`ALTER TABLE objects ADD COLUMN email_water TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN email_energy TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN finance_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE meters ADD COLUMN parent_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE meters ADD COLUMN previous_serial TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE meters ADD COLUMN replaced_at TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = tx.Exec(q)
	}
	if !tableHasColumn(tx, "meters", "finance_enabled") {
		_, _ = tx.Exec(`ALTER TABLE meters ADD COLUMN finance_enabled INTEGER NOT NULL DEFAULT 0`)
		_, _ = tx.Exec(`UPDATE meters SET finance_enabled = 1 WHERE object_id IN (SELECT id FROM objects WHERE finance_enabled = 1)`)
	}
	if !tableHasColumn(tx, "meters", "finance_mode") {
		_, _ = tx.Exec(`ALTER TABLE meters ADD COLUMN finance_mode TEXT NOT NULL DEFAULT ''`)
		_, _ = tx.Exec(`UPDATE meters SET finance_mode = CASE WHEN finance_enabled = 1 THEN 'both' ELSE 'none' END`)
	}
	_, _ = tx.Exec(`UPDATE OR IGNORE money SET account = 'water_cold' WHERE account = 'water'`)
	_, _ = tx.Exec(`DELETE FROM money WHERE account = 'water'`)
	hasAdvance := tableHasColumn(tx, "money", "advance")
	if !hasAdvance {
		_, _ = tx.Exec(`ALTER TABLE money ADD COLUMN advance REAL NOT NULL DEFAULT 0`)
		_, _ = tx.Exec(`UPDATE money SET advance = CASE WHEN paid >= charged THEN paid - charged ELSE paid END`)
	}
	if !tableHasColumn(tx, "money", "paid_flag") {
		_, _ = tx.Exec(`ALTER TABLE money ADD COLUMN paid_flag INTEGER NOT NULL DEFAULT 0`)
		_, _ = tx.Exec(`UPDATE money SET paid_flag = CASE WHEN charged > 0 AND paid >= charged THEN 1 ELSE 0 END`)
	}
	if !tableHasColumn(tx, "money", "advance_paid") {
		_, _ = tx.Exec(`ALTER TABLE money ADD COLUMN advance_paid INTEGER NOT NULL DEFAULT 0`)
		_, _ = tx.Exec(`UPDATE money SET advance_paid = 1 WHERE advance > 0`)
	}
	// Старые объекты, у которых уже есть начисления, оставляем с включённым финучётом.
	_, _ = tx.Exec(`UPDATE objects SET finance_enabled = 1 WHERE id IN (SELECT DISTINCT object_id FROM money)`)
	var version int
	_ = tx.QueryRow("PRAGMA user_version").Scan(&version)
	if version < schemaVersion {
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return fmt.Errorf("миграция: %w", err)
		}
	}
	return tx.Commit()
}

func tableHasColumn(tx *sql.Tx, table, column string) bool {
	rows, err := tx.Query("PRAGMA table_info('" + table + "')")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}
func (s *Store) quickCheck() error {
	var res string
	if err := s.db.QueryRow("PRAGMA quick_check(1)").Scan(&res); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(res), "ok") {
		return fmt.Errorf("%s", res)
	}
	return nil
}

// ---------------------------------------------------------------- объекты

// Objects возвращает объекты, при includeArchived — вместе с архивными.
func (s *Store) Objects(includeArchived bool) ([]model.Object, error) {
	q := `SELECT id, name, kind, address, owner, account_water, account_energy,
		email_water, email_energy, finance_enabled, note, archived, created_at
		FROM objects`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY archived, name, id`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Object
	for rows.Next() {
		o, err := scanObject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanObject(r rowScanner) (model.Object, error) {
	var o model.Object
	var archived int
	var created string
	var finance int
	err := r.Scan(&o.ID, &o.Name, &o.Kind, &o.Address, &o.Owner,
		&o.AccountWater, &o.AccountEnergy, &o.EmailWater, &o.EmailEnergy,
		&finance, &o.Note, &archived, &created)
	o.FinanceEnabled = finance != 0
	o.Archived = archived != 0
	o.CreatedAt = parseTime(created)
	return o, err
}

// Object возвращает объект по id.
func (s *Store) Object(id int64) (model.Object, error) {
	row := s.db.QueryRow(`SELECT id, name, kind, address, owner, account_water, account_energy,
		email_water, email_energy, finance_enabled, note, archived, created_at
		FROM objects WHERE id = ?`, id)
	o, err := scanObject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Object{}, ErrNotFound
	}
	return o, err
}

// SaveObject создаёт или обновляет объект.
func (s *Store) SaveObject(o *model.Object) error {
	if o.Kind == "" {
		o.Kind = model.KindHouse
	}
	if o.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO objects (name, kind, address, owner, account_water, account_energy,
			email_water, email_energy, finance_enabled, note, archived, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			o.Name, o.Kind, o.Address, o.Owner, o.AccountWater, o.AccountEnergy,
			o.EmailWater, o.EmailEnergy, boolInt(o.FinanceEnabled), o.Note,
			boolInt(o.Archived), time.Now().In(s.loc).Format(time.RFC3339))
		if err != nil {
			return err
		}
		o.ID, err = res.LastInsertId()
		return err
	}
	_, err := s.db.Exec(`UPDATE objects SET name=?, kind=?, address=?, owner=?, account_water=?,
		account_energy=?, email_water=?, email_energy=?, finance_enabled=?, note=?, archived=? WHERE id=?`,
		o.Name, o.Kind, o.Address, o.Owner, o.AccountWater, o.AccountEnergy,
		o.EmailWater, o.EmailEnergy, boolInt(o.FinanceEnabled), o.Note, boolInt(o.Archived), o.ID)
	return err
}

// ArchiveObject скрывает объект, не удаляя историю.
func (s *Store) ArchiveObject(id int64) error {
	_, err := s.db.Exec(`UPDATE objects SET archived = 1 WHERE id = ?`, id)
	return err
}

// DeleteObject удаляет объект вместе с приборами и показаниями.
func (s *Store) DeleteObject(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM readings WHERE meter_id IN (SELECT id FROM meters WHERE object_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM meters WHERE object_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM objects WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- приборы

// Meters возвращает приборы объекта.
func (s *Store) Meters(objectID int64, includeArchived bool) ([]model.Meter, error) {
	q := `SELECT id, object_id, resource, label, brand, serial, start_value, installed_on, parent_id, finance_enabled, finance_mode, archived, sort, previous_serial, replaced_at
		FROM meters WHERE object_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY sort, id`
	rows, err := s.db.Query(q, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Meter
	for rows.Next() {
		m, err := scanMeter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortMeters(out)
	return out, nil
}

func sortMeters(ms []model.Meter) {
	rank := map[model.Resource]int{}
	for i, r := range model.ResourceOrder {
		rank[r] = i
	}
	sort.SliceStable(ms, func(i, j int) bool {
		ri, rj := rank[ms[i].Resource], rank[ms[j].Resource]
		if ri != rj {
			return ri < rj
		}
		if ms[i].Sort != ms[j].Sort {
			return ms[i].Sort < ms[j].Sort
		}
		return ms[i].ID < ms[j].ID
	})
}

func scanMeter(r rowScanner) (model.Meter, error) {
	var m model.Meter
	var archived, finance int
	err := r.Scan(&m.ID, &m.ObjectID, &m.Resource, &m.Label, &m.Brand, &m.Serial,
		&m.StartValue, &m.InstalledOn, &m.ParentID, &finance, &m.FinanceMode, &archived, &m.Sort,
		&m.PreviousSerial, &m.ReplacedAt)
	m.Archived = archived != 0
	m.FinanceEnabled = finance != 0
	return m, err
}

// Meter возвращает прибор по id.
func (s *Store) Meter(id int64) (model.Meter, error) {
	row := s.db.QueryRow(`SELECT id, object_id, resource, label, brand, serial, start_value, installed_on, parent_id, finance_enabled, finance_mode, archived, sort, previous_serial, replaced_at
		FROM meters WHERE id = ?`, id)
	m, err := scanMeter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Meter{}, ErrNotFound
	}
	return m, err
}

// SaveMeter создаёт или обновляет прибор.
func (s *Store) SaveMeter(m *model.Meter) error {
	if !model.ValidResource(string(m.Resource)) {
		return fmt.Errorf("неизвестный ресурс")
	}
	if m.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO meters (object_id, resource, label, brand, serial, start_value, installed_on, parent_id, finance_enabled, finance_mode, archived, sort, previous_serial, replaced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ObjectID, m.Resource, m.Label, m.Brand, m.Serial, m.StartValue, m.InstalledOn,
			m.ParentID, boolInt(m.FinanceEnabled), m.FinanceModeValue(), boolInt(m.Archived), m.Sort, m.PreviousSerial, m.ReplacedAt)
		if err != nil {
			return err
		}
		m.ID, err = res.LastInsertId()
		return err
	}
	_, err := s.db.Exec(`UPDATE meters SET resource=?, label=?, brand=?, serial=?, start_value=?,
		installed_on=?, parent_id=?, finance_enabled=?, finance_mode=?, archived=?, sort=?, previous_serial=?, replaced_at=? WHERE id=?`,
		m.Resource, m.Label, m.Brand, m.Serial, m.StartValue, m.InstalledOn,
		m.ParentID, boolInt(m.FinanceEnabled), m.FinanceModeValue(), boolInt(m.Archived), m.Sort, m.PreviousSerial, m.ReplacedAt, m.ID)
	return err
}

// ReadingsAfter возвращает периоды с показаниями начиная с периода from
// (включительно). Пустой from — все периоды.
func (s *Store) ReadingsAfter(meterID int64, from string) ([]string, error) {
	q := `SELECT period FROM readings WHERE meter_id = ?`
	args := []any{meterID}
	if from != "" {
		q += ` AND period >= ?`
		args = append(args, from)
	}
	q += ` ORDER BY period`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ArchiveMeter снимает прибор с учёта, сохраняя историю.
func (s *Store) ArchiveMeter(id int64) error {
	_, err := s.db.Exec(`UPDATE meters SET archived = 1 WHERE id = ?`, id)
	return err
}

// DeleteMeter убирает прибор, сохраняя введённые показания: они остаются
// в истории объекта. Суб-счётчики удалённого прибора становятся самостоятельными.
func (s *Store) DeleteMeter(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE meters SET parent_id = 0 WHERE parent_id = ?`, id); err != nil {
		return err
	}
	// Показания остаются: прибор помечается архивным, чтобы история
	// отображалась в журнале, отчётах и ведомости как неактивная.
	if _, err := tx.Exec(`UPDATE meters SET archived = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- показания

// ReadingsForObject возвращает все показания объекта: meterID -> period -> value.
func (s *Store) ReadingsForObject(objectID int64) (map[int64]map[string]float64, error) {
	rows, err := s.db.Query(`SELECT r.meter_id, r.period, r.value
		FROM readings r JOIN meters m ON m.id = r.meter_id
		WHERE m.object_id = ?`, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string]float64{}
	for rows.Next() {
		var meterID int64
		var period string
		var value float64
		if err := rows.Scan(&meterID, &period, &value); err != nil {
			return nil, err
		}
		if out[meterID] == nil {
			out[meterID] = map[string]float64{}
		}
		out[meterID][period] = value
	}
	return out, rows.Err()
}

// ReadingsForMonth возвращает показания месяца по приборам объекта.
func (s *Store) ReadingsForMonth(objectID int64, period string) (map[int64]model.Reading, error) {
	rows, err := s.db.Query(`SELECT r.meter_id, r.period, r.value, r.note, r.updated_at
		FROM readings r JOIN meters m ON m.id = r.meter_id
		WHERE m.object_id = ? AND r.period = ?`, objectID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]model.Reading{}
	for rows.Next() {
		var r model.Reading
		var updated string
		if err := rows.Scan(&r.MeterID, &r.Period, &r.Value, &r.Note, &updated); err != nil {
			return nil, err
		}
		r.UpdatedAt = parseTime(updated)
		out[r.MeterID] = r
	}
	return out, rows.Err()
}

// SaveReading записывает показание прибора за месяц.
func (s *Store) SaveReading(meterID int64, period string, value float64, note string) error {
	_, err := s.db.Exec(`INSERT INTO readings (meter_id, period, value, note, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(meter_id, period) DO UPDATE SET
			value = excluded.value, note = excluded.note, updated_at = excluded.updated_at`,
		meterID, period, value, note, time.Now().In(s.loc).Format(time.RFC3339))
	return err
}

// DeleteReading удаляет показание.
func (s *Store) DeleteReading(meterID int64, period string) error {
	_, err := s.db.Exec(`DELETE FROM readings WHERE meter_id = ? AND period = ?`, meterID, period)
	return err
}

// PeriodRange возвращает минимальный и максимальный периоды с данными.
func (s *Store) PeriodRange(objectID int64) (string, string, bool) {
	var minP, maxP sql.NullString
	err := s.db.QueryRow(`SELECT MIN(r.period), MAX(r.period)
		FROM readings r JOIN meters m ON m.id = r.meter_id WHERE m.object_id = ?`, objectID).Scan(&minP, &maxP)
	if err != nil || !minP.Valid || !maxP.Valid {
		return "", "", false
	}
	return minP.String, maxP.String, true
}

// ---------------------------------------------------------------- настройки

// Setting читает настройку с значением по умолчанию.
func (s *Store) Setting(key, def string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

// SetSetting сохраняет настройку.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---------------------------------------------------------------- утилиты

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

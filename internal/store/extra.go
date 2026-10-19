package store

import (
	"os"
	"time"
)

// ReadingInput — значение для пакетного сохранения.
type ReadingInput struct {
	MeterID int64
	Value   float64
	Note    string
}

// SaveReadings сохраняет набор показаний одной транзакцией.
func (s *Store) SaveReadings(inputs []ReadingInput, period string) error {
	if len(inputs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().In(s.loc).Format(time.RFC3339)
	for _, in := range inputs {
		if _, err := tx.Exec(`INSERT INTO readings (meter_id, period, value, note, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(meter_id, period) DO UPDATE SET
				value = excluded.value, note = excluded.note, updated_at = excluded.updated_at`,
			in.MeterID, period, in.Value, in.Note, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteMonth удаляет все показания объекта за месяц.
func (s *Store) DeleteMonth(objectID int64, period string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM readings WHERE period = ? AND meter_id IN
		(SELECT id FROM meters WHERE object_id = ?)`, period, objectID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Stats — сводка по базе для страницы настроек.
type Stats struct {
	Objects  int
	Meters   int
	Readings int
	Money    int
	DBBytes  int64
	Oldest   string
	Newest   string
}

// Stats собирает краткую статистику.
func (s *Store) Stats() Stats {
	var st Stats
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM objects WHERE archived = 0`).Scan(&st.Objects)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM meters WHERE archived = 0`).Scan(&st.Meters)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&st.Readings)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM money`).Scan(&st.Money)
	if info, err := os.Stat(s.path); err == nil {
		st.DBBytes = info.Size()
	}
	_ = s.db.QueryRow(`SELECT COALESCE(MIN(period), ''), COALESCE(MAX(period), '') FROM readings`).
		Scan(&st.Oldest, &st.Newest)
	return st
}

// ExportRows возвращает плоскую выгрузку показаний.
type ExportRow struct {
	Object   string
	Address  string
	Owner    string
	Meter    string
	Serial   string
	Resource string
	Period   string
	Value    float64
}

// AllReadingsForExport возвращает все показания с расшифровкой.
func (s *Store) AllReadingsForExport() ([]ExportRow, error) {
	rows, err := s.db.Query(`SELECT o.name, o.address, o.owner, m.label, m.serial, m.resource,
		r.period, r.value
		FROM readings r
		JOIN meters m ON m.id = r.meter_id
		JOIN objects o ON o.id = m.object_id
		ORDER BY o.name, m.resource, m.id, r.period`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRow
	for rows.Next() {
		var e ExportRow
		if err := rows.Scan(&e.Object, &e.Address, &e.Owner, &e.Meter, &e.Serial,
			&e.Resource, &e.Period, &e.Value); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

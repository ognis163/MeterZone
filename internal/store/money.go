package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"indextro/internal/model"
)

// MoneyForMonth возвращает начисления и оплаты объекта за месяц.
func (s *Store) MoneyForMonth(objectID int64, period string) (map[model.Account]model.Money, error) {
	rows, err := s.db.Query(`SELECT object_id, period, account, charged, paid, advance, advance_paid, paid_flag, note, updated_at
		FROM money WHERE object_id = ? AND period = ?`, objectID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[model.Account]model.Money{}
	for rows.Next() {
		var m model.Money
		var updated string
		var paidFlag, advancePaid int
		if err := rows.Scan(&m.ObjectID, &m.Period, &m.Account, &m.Charged, &m.Paid,
			&m.AdvanceSum, &advancePaid, &paidFlag, &m.Note, &updated); err != nil {
			return nil, err
		}
		m.PaidFlag = paidFlag != 0
		m.AdvancePaid = advancePaid != 0
		m.AdvancePaid = advancePaid != 0
		m.UpdatedAt = parseTime(updated)
		out[m.Account] = m
	}
	return out, rows.Err()
}

// SaveMoney сохраняет счета, авансы и отметки об оплате за месяц.
// Частичное обновление: сохраняются только переданные счета, остальные не трогаются.
func (s *Store) SaveMoney(items []model.Money, period string) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().In(s.loc).Format(time.RFC3339)
	for _, m := range items {
		if !model.ValidAccount(string(m.Account)) {
			return fmt.Errorf("неизвестный счёт")
		}
		// Совместимость со старыми вызовами: передан только итог оплаты.
		if m.AdvanceSum == 0 && !m.PaidFlag && m.Paid > 0 {
			if m.Charged > 0 && m.Paid >= m.Charged {
				m.PaidFlag = true
				m.AdvanceSum = model.Round2(m.Paid - m.Charged)
			} else {
				m.AdvanceSum = model.Round2(m.Paid)
			}
		}
		m.AdvanceSum = model.Round2(m.AdvanceSum)
		m.Paid = m.AdvanceSum
		if m.PaidFlag {
			m.Paid = model.Round2(m.Paid + m.Charged)
		}
		if m.Charged == 0 && m.Paid == 0 && m.AdvanceSum == 0 && m.Note == "" {
			if _, err := tx.Exec(`DELETE FROM money WHERE object_id = ? AND period = ? AND account = ?`,
				m.ObjectID, period, m.Account); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`INSERT INTO money (object_id, period, account, charged, paid, advance, advance_paid, paid_flag, note, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(object_id, period, account) DO UPDATE SET
				charged = excluded.charged, paid = excluded.paid,
				advance = excluded.advance, advance_paid = excluded.advance_paid, paid_flag = excluded.paid_flag,
				note = excluded.note, updated_at = excluded.updated_at`,
			m.ObjectID, period, m.Account, m.Charged, m.Paid, m.AdvanceSum,
			boolInt(m.AdvancePaid), boolInt(m.PaidFlag), m.Note, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PatchMoney меняет отдельные поля одного счёта, не перезаписывая остальные.
// Функция merge вызывается с текущим значением (или пустым, если записи нет).
func (s *Store) PatchMoney(objectID int64, period string, account model.Account, merge func(*model.Money)) error {
	if !model.ValidAccount(string(account)) {
		return fmt.Errorf("неизвестный счёт")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	m := model.Money{ObjectID: objectID, Period: period, Account: account}
	var updated string
	var paidFlag, advancePaid int
	err = tx.QueryRow(`SELECT charged, paid, advance, advance_paid, paid_flag, note, updated_at
		FROM money WHERE object_id = ? AND period = ? AND account = ?`,
		objectID, period, account).Scan(&m.Charged, &m.Paid, &m.AdvanceSum, &advancePaid, &paidFlag, &m.Note, &updated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	m.PaidFlag = paidFlag != 0
	m.AdvancePaid = advancePaid != 0

	merge(&m)
	m.Charged = model.Round2(m.Charged)
	m.AdvanceSum = model.Round2(m.AdvanceSum)
	m.Paid = m.AdvanceSum
	if m.PaidFlag {
		m.Paid = model.Round2(m.Paid + m.Charged)
	}
	if m.Charged == 0 && m.Paid == 0 && m.AdvanceSum == 0 && m.Note == "" {
		if _, err := tx.Exec(`DELETE FROM money WHERE object_id = ? AND period = ? AND account = ?`,
			objectID, period, account); err != nil {
			return err
		}
		return tx.Commit()
	}
	now := time.Now().In(s.loc).Format(time.RFC3339)
	if _, err := tx.Exec(`INSERT INTO money (object_id, period, account, charged, paid, advance, advance_paid, paid_flag, note, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(object_id, period, account) DO UPDATE SET
			charged = excluded.charged, paid = excluded.paid,
			advance = excluded.advance, advance_paid = excluded.advance_paid,
			paid_flag = excluded.paid_flag,
			note = excluded.note, updated_at = excluded.updated_at`,
		m.ObjectID, period, m.Account, m.Charged, m.Paid, m.AdvanceSum,
		boolInt(m.AdvancePaid), boolInt(m.PaidFlag), m.Note, now); err != nil {
		return err
	}
	return tx.Commit()
}

// MoneyTotals суммирует начисления и оплаты объекта за период.
func (s *Store) MoneyTotals(objectID int64, from, to model.Period) (map[model.Account]model.Money, error) {
	rows, err := s.db.Query(`SELECT account, COALESCE(SUM(charged), 0), COALESCE(SUM(paid), 0)
		FROM money WHERE object_id = ? AND period >= ? AND period <= ?
		GROUP BY account`, objectID, from.String(), to.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[model.Account]model.Money{}
	for rows.Next() {
		var m model.Money
		if err := rows.Scan(&m.Account, &m.Charged, &m.Paid); err != nil {
			return nil, err
		}
		out[m.Account] = m
	}
	return out, rows.Err()
}

// AvailableMonths возвращает месяцы, за которые есть хотя бы одно показание.
func (s *Store) AvailableMonths(objectID int64) ([]model.Period, error) {
	rows, err := s.db.Query(`SELECT DISTINCT r.period
		FROM readings r JOIN meters m ON m.id = r.meter_id
		WHERE m.object_id = ? ORDER BY r.period`, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Period
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		period, err := model.ParsePeriod(p)
		if err == nil {
			out = append(out, period)
		}
	}
	return out, rows.Err()
}

// HasReadings сообщает, есть ли у объекта показания за месяц.
func (s *Store) HasReadings(objectID int64, period string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM readings r JOIN meters m ON m.id = r.meter_id
		WHERE m.object_id = ? AND r.period = ?`, objectID, period).Scan(&n)
	return n > 0
}

// AvailableMonthsAll возвращает все месяцы, за которые есть показания.
func (s *Store) AvailableMonthsAll() ([]model.Period, error) {
	rows, err := s.db.Query(`SELECT DISTINCT period FROM readings ORDER BY period`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Period
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if period, err := model.ParsePeriod(p); err == nil {
			out = append(out, period)
		}
	}
	return out, rows.Err()
}

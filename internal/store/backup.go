package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup — файл резервной копии базы.
type Backup struct {
	Name string
	Path string
	Size int64
	Time time.Time
}

// DefaultBackupDir возвращает каталог копий рядом с базой.
func DefaultBackupDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "backups")
}

// Backup делает согласованную копию базы через VACUUM INTO.
func (s *Store) Backup(dir string) (Backup, error) {
	if dir == "" {
		dir = DefaultBackupDir(s.path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Backup{}, fmt.Errorf("каталог копий: %w", err)
	}
	now := time.Now().In(s.loc)
	name := fmt.Sprintf("meterzone-%s.db", now.Format("20060102-150405"))
	dst := filepath.Join(dir, name)

	// На случай совпадения имён в одну секунду.
	for i := 2; fileExists(dst); i++ {
		name = fmt.Sprintf("meterzone-%s-%d.db", now.Format("20060102-150405"), i)
		dst = filepath.Join(dir, name)
	}

	if _, err := s.db.Exec(`VACUUM INTO ?`, dst); err != nil {
		// Резервный вариант для драйверов без параметров в VACUUM.
		escaped := strings.ReplaceAll(dst, "'", "''")
		if _, err2 := s.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped)); err2 != nil {
			return Backup{}, fmt.Errorf("резервная копия: %v", err)
		}
	}
	info, err := os.Stat(dst)
	if err != nil {
		return Backup{}, err
	}
	return Backup{Name: name, Path: dst, Size: info.Size(), Time: info.ModTime()}, nil
}

// ListBackups возвращает копии, сначала новые.
func (s *Store) ListBackups(dir string) ([]Backup, error) {
	if dir == "" {
		dir = DefaultBackupDir(s.path)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Backup
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{
			Name: e.Name(),
			Path: filepath.Join(dir, e.Name()),
			Size: info.Size(),
			Time: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

// PruneBackups удаляет старые копии, оставляя keep самых новых.
func (s *Store) PruneBackups(dir string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	list, err := s.ListBackups(dir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for i, b := range list {
		if i < keep {
			continue
		}
		if err := os.Remove(b.Path); err == nil {
			removed = append(removed, b.Name)
		}
	}
	return removed, nil
}

// BackupDue проверяет, что с момента последней копии прошло больше interval.
func (s *Store) BackupDue(dir string, interval time.Duration) bool {
	list, err := s.ListBackups(dir)
	if err != nil || len(list) == 0 {
		return true
	}
	return time.Since(list[0].Time) > interval
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ValidateDBFile проверяет, что файл является корректной базой SQLite.
func ValidateDBFile(path string) error {
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var res string
	if err := db.QueryRow("PRAGMA quick_check(1)").Scan(&res); err != nil {
		return fmt.Errorf("файл не является базой SQLite: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(res), "ok") {
		return fmt.Errorf("база повреждена: %s", res)
	}
	return nil
}

// BackupDueMonth проверяет, что в этом календарном месяце копии ещё не было.
func (s *Store) BackupDueMonth(dir string) bool {
	list, err := s.ListBackups(dir)
	if err != nil || len(list) == 0 {
		return true
	}
	last := list[0].Time.In(s.loc)
	now := time.Now().In(s.loc)
	return last.Year() != now.Year() || last.Month() != now.Month()
}

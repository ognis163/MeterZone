package store

import (
	"os"
	"testing"
	"time"
)

func TestBackupDueMonth(t *testing.T) {
	st := newStore(t)
	dir := t.TempDir()
	if !st.BackupDueMonth(dir) {
		t.Fatal("без копий месяц должен быть не закрыт")
	}
	b, err := st.Backup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.BackupDueMonth(dir) {
		t.Fatal("после копии в этом месяце новая не нужна")
	}

	prev := time.Now().AddDate(0, -1, -1)
	if err := os.Chtimes(b.Path, prev, prev); err != nil {
		t.Fatal(err)
	}
	if !st.BackupDueMonth(dir) {
		t.Fatal("копия прошлого месяца не должна закрывать текущий")
	}
}

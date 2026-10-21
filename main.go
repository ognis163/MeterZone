// MeterZone — учёт показаний приборов учёта.
// Один статический бинарник: SQLite, PDF, веб-интерфейс.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"indextro/internal/store"
	WebCl "indextro/internal/web"
)

const version = "1.1"

//go:embed DejaVuSans.ttf
var fontBytes []byte

func main() {
	host := flag.String("host", env("HOST", "0.0.0.0"), "адрес прослушивания")
	port := flag.Int("port", envInt("PORT", 8099), "порт HTTP")
	dbPath := flag.String("db", env("APP_DB", ""), "путь к файлу базы (по умолчанию data/meterzone.db рядом с программой)")
	backupDir := flag.String("backups", env("APP_BACKUP_DIR", ""), "каталог резервных копий")
	showVersion := flag.Bool("version", false, "показать версию и выйти")
	demo := flag.Bool("demo", false, "заполнить пустую базу демонстрационными данными")
	flag.Parse()

	if *showVersion {
		fmt.Println("meterzone", version)
		return
	}

	loc := resolveLocation()
	db := resolveDBPath(*dbPath)
	backups := *backupDir
	if backups == "" {
		backups = store.DefaultBackupDir(db)
	}

	log.SetFlags(log.Ldate | log.Ltime)
	log.Printf("MeterZone %s · %s", version, loc)

	st, err := store.Open(db, loc)
	if err != nil {
		log.Fatalf("база данных: %v", err)
	}
	defer st.Close()

	autoImportLegacy(st)
	if *demo {
		if err := st.SeedDemo(); err != nil {
			log.Printf("демо-данные: %v", err)
		} else {
			log.Printf("демо-данные созданы")
		}
	}

	runtimePort := *port
	if v := strings.TrimSpace(st.Setting("http_port", "")); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
			runtimePort = p
		}
	}

	srv, err := WebCl.New(st, loc, version, fontBytes, backups, runtimePort)
	if err != nil {
		log.Fatalf("шаблоны: %v", err)
	}

	// Копия раз в календарный месяц: при первом запуске в месяце и далее проверка раз в 6 часов.
	notify := func(path string) { srv.NotifyBackup(path) }
	makeBackup(st, backups, notify)
	if removed, err := st.PruneBackups(backups, 14); err == nil && len(removed) > 0 {
		log.Printf("удалены старые копии: %d", len(removed))
	}
	go backupLoop(st, backups, notify)

	binder := newPortBinder(*host, srv.Handler())
	srv.OnPortChange = binder.Change
	if err := binder.Start(runtimePort); err != nil {
		log.Fatalf("не удалось занять порт %d: %v", runtimePort, err)
	}

	log.Printf("база: %s", db)
	log.Printf("резервные копии: %s", backups)
	log.Printf("сервер запущен на http://%s", displayAddr(*host, runtimePort))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Printf("остановка…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	binder.Shutdown(shutdownCtx)
}

func displayAddr(host string, port int) string {
	h := host
	if h == "0.0.0.0" || h == "" || h == "::" {
		h = localIP()
	}
	return net.JoinHostPort(h, strconv.Itoa(port))
}

// localIP возвращает первый частный IPv4-адрес машины.
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip := ipnet.IP.To4()
		if ip != nil && ip.IsPrivate() {
			return ip.String()
		}
	}
	return "127.0.0.1"
}

func resolveLocation() *time.Location {
	name := strings.TrimSpace(env("APP_TZ", env("TZ", "")))
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
		log.Printf("часовой пояс %q не найден, используется локальный", name)
	}
	return time.Local
}

func resolveDBPath(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue
	}
	base := "."
	if exe, err := os.Executable(); err == nil {
		base = filepath.Dir(exe)
	}
	return filepath.Join(base, "data", "meterzone.db")
}

func autoImportLegacy(st *store.Store) {
	if st.Stats().Objects > 0 {
		return
	}
	path := strings.TrimSpace(os.Getenv("LEGACY_DB"))
	if path == "" {
		path = "/opt/indextro/indextro.db"
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	stats, err := st.ImportLegacy(path)
	if err != nil {
		log.Printf("импорт старой базы %s не выполнен: %v", path, err)
		return
	}
	log.Printf("импортирована старая база %s: объектов %d, приборов %d, показаний %d",
		path, stats.Objects, stats.Meters, stats.Readings)
	if err := st.SetSetting("legacy_source", path); err == nil {
		_ = st.SetSetting("legacy_imported_at", time.Now().Format(time.RFC3339))
	}
}

func makeBackup(st *store.Store, dir string, notify func(string)) {
	if !st.BackupDueMonth(dir) {
		return
	}
	b, err := st.Backup(dir)
	if err != nil {
		log.Printf("резервная копия: %v", err)
		return
	}
	log.Printf("резервная копия за месяц: %s (%d КБ)", b.Name, b.Size/1024)
	if notify != nil {
		notify(b.Path)
	}
	if removed, err := st.PruneBackups(dir, 14); err == nil && len(removed) > 0 {
		log.Printf("удалены старые копии: %d", len(removed))
	}
}

func backupLoop(st *store.Store, dir string, notify func(string)) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for range t.C {
		makeBackup(st, dir, notify)
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

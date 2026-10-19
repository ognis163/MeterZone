package web

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"indextro/internal/mail"
)

// NotifyBackup отправляет свежую резервную копию на почту, если это включено.
func (s *Server) NotifyBackup(path string) {
	if s.Store.Setting("backup_email_enabled", "") != "1" {
		return
	}
	to := strings.TrimSpace(s.Store.Setting("backup_email", ""))
	if to == "" {
		return
	}
	cfg := s.loadSMTP()
	if err := cfg.config().Validate(); err != nil {
		log.Printf("копия на почту: %v", err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("копия на почту: %v", err)
		return
	}
	name := filepath.Base(path)
	now := time.Now().In(s.Loc)
	subject := "MeterZone: резервная копия " + now.Format("02.01.2006 15:04")
	body := "Во вложении резервная копия базы MeterZone от " + now.Format("02.01.2006 15:04") + "."
	if err := mail.SendFile(cfg.config(), to, subject, body, name, data, "application/x-sqlite3"); err != nil {
		log.Printf("копия на почту: %v", err)
		return
	}
	log.Printf("резервная копия отправлена на %s", to)
}

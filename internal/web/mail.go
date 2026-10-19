package web

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"strconv"
	"strings"

	mailer "indextro/internal/mail"
)

// smtpSettings — настройки почты, которые хранятся в базе.
type smtpSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	TLS      string `json:"tls"`
}

func defaultSMTP() smtpSettings {
	return smtpSettings{
		Host:     "smtp.yandex.ru",
		Port:     465,
		FromName: "MeterZone",
		TLS:      "auto",
	}
}

func (s *Server) loadSMTP() smtpSettings {
	cfg := defaultSMTP()
	raw := s.Store.Setting("smtp", "")
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	if cfg.Port == 0 {
		cfg.Port = 465
	}
	if cfg.TLS == "" {
		cfg.TLS = "auto"
	}
	return cfg
}

func (s *Server) saveSMTP(cfg smtpSettings) error {
	if cfg.Port == 0 {
		cfg.Port = 465
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.Store.SetSetting("smtp", string(raw))
}

func (c smtpSettings) config() mailer.Config {
	return mailer.Config{
		Host:     c.Host,
		Port:     c.Port,
		User:     c.User,
		Password: c.Password,
		From:     c.From,
		FromName: c.FromName,
		TLS:      c.TLS,
	}
}

// smtpView — данные для страницы настроек (без пароля).
type smtpView struct {
	Host        string
	Port        int
	User        string
	From        string
	FromName    string
	TLS         string
	HasPassword bool
	Ready       bool
}

func smtpToView(c smtpSettings) smtpView {
	return smtpView{
		Host:        c.Host,
		Port:        c.Port,
		User:        c.User,
		From:        c.From,
		FromName:    c.FromName,
		TLS:         c.TLS,
		HasPassword: c.Password != "",
		Ready:       c.Host != "" && c.From != "",
	}
}

func validEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	_, err := mail.ParseAddress(s)
	return err == nil
}

// parseMoneyForm читает сумму из формы; пустая строка — ноль.
func parseMoney(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(raw, ",", "."), " ", ""), 64)
	if err != nil {
		return 0, fmt.Errorf("введите сумму")
	}
	if v < 0 {
		return 0, fmt.Errorf("сумма не может быть отрицательной")
	}
	if v > 1_000_000_000 {
		return 0, fmt.Errorf("слишком большая сумма")
	}
	return v, nil
}

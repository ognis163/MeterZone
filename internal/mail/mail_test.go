package mail

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	good := Config{Host: "smtp.example.ru", Port: 465, From: "a@example.ru", TLS: "auto"}
	if err := good.Validate(); err != nil {
		t.Fatalf("good: %v", err)
	}
	for _, bad := range []Config{
		{Host: "", Port: 465, From: "a@example.ru"},
		{Host: "smtp", Port: 0, From: "a@example.ru"},
		{Host: "smtp", Port: 465, From: "не-адрес"},
		{Host: "smtp", Port: 465, From: "a@example.ru", TLS: "weird"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("ожидалась ошибка для %+v", bad)
		}
	}
}

func TestBuildMessage(t *testing.T) {
	cfg := Config{From: "uchet@example.ru", FromName: "Учёт дома"}
	body := "Здравствуйте!\nПоказания за октябрь."
	attach := []byte("%PDF-1.4 test")
	msg := buildMessage(cfg, "vodokanal@example.ru", "Показания — Дом — октябрь 2026",
		body, "Сведения.pdf", attach, "application/pdf")
	s := string(msg)
	for _, want := range []string{
		"From: =?utf-8?",
		"To: vodokanal@example.ru",
		"Subject: =?utf-8?",
		"Content-Type: multipart/mixed;",
		"application/pdf",
		"Content-Transfer-Encoding: base64",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("нет %q", want)
		}
	}
	if strings.Contains(s, "\n") && !strings.Contains(s, "\r\n") {
		t.Error("строки должны завершаться CRLF")
	}
	// Тело и вложение должны быть в base64.
	flat := strings.ReplaceAll(s, "\r\n", "")
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	if !bytes.Contains([]byte(flat), []byte(enc)) {
		t.Error("тело письма не закодировано в base64 UTF-8")
	}
}
func TestAttachmentFileNameASCII(t *testing.T) {
	cfg := Config{From: "uchet@example.ru"}
	msg := buildMessage(cfg, "to@example.ru", "Копия", "body",
		"meterzone-20261019-120000.db", []byte("SQLite format 3\x00"), "application/x-sqlite3")
	s := string(msg)
	for _, want := range []string{
		`filename="meterzone-20261019-120000.db"`,
		"application/x-sqlite3",
		"filename*=UTF-8''meterzone-20261019-120000.db",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("нет %q", want)
		}
	}
}

// Package mail отправляет письма с PDF-вложением через SMTP.
package mail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config — настройки SMTP.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string // адрес отправителя
	FromName string
	TLS      string // auto | ssl | starttls | none
}

// Validate проверяет обязательные поля.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("не указан SMTP-сервер")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("некорректный порт SMTP")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("некорректный адрес отправителя")
	}
	switch c.TLS {
	case "", "auto", "ssl", "starttls", "none":
	default:
		return fmt.Errorf("неизвестный режим TLS")
	}
	return nil
}

func (c Config) useSSL() bool {
	if c.TLS == "ssl" {
		return true
	}
	if c.TLS == "auto" || c.TLS == "" {
		return c.Port == 465
	}
	return false
}

func (c Config) dial() (net.Conn, error) {
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	d := &net.Dialer{Timeout: 15 * time.Second}
	if c.useSSL() {
		return tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
	}
	return d.Dial("tcp", addr)
}

func (c Config) client() (*smtp.Client, net.Conn, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, nil, err
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if !c.useSSL() && c.TLS != "none" {
		if ok, _ := cl.Extension("STARTTLS"); ok {
			if err := cl.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
				cl.Close()
				return nil, nil, err
			}
		} else if c.TLS == "starttls" {
			cl.Close()
			return nil, nil, fmt.Errorf("сервер не поддерживает STARTTLS")
		}
	}
	if c.User != "" {
		auth := smtp.PlainAuth("", c.User, c.Password, c.Host)
		if err := cl.Auth(auth); err != nil {
			cl.Close()
			return nil, nil, fmt.Errorf("авторизация: %w", err)
		}
	}
	return cl, conn, nil
}

// Check подключается к SMTP и проверяет авторизацию.
func Check(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	cl, conn, err := cfg.client()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = cl.Quit()
	return nil
}

// Send отправляет PDF-вложение.
func Send(cfg Config, to, subject, body, attachName string, attach []byte) error {
	return SendFile(cfg, to, subject, body, attachName, attach, "application/pdf")
}

// SendFile отправляет письмо с произвольным вложением.
func SendFile(cfg Config, to, subject, body, attachName string, attach []byte, contentType string) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	to = strings.TrimSpace(to)
	addr, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("некорректный адрес получателя")
	}
	cl, conn, err := cfg.client()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := cl.Mail(cfg.From); err != nil {
		return err
	}
	if err := cl.Rcpt(addr.Address); err != nil {
		return err
	}
	wc, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(buildMessage(cfg, addr.Address, subject, body, attachName, attach, contentType)); err != nil {
		wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

func buildMessage(cfg Config, to, subject, body, attachName string, attach []byte, contentType string) []byte {
	var b bytes.Buffer
	boundary := randomBoundary()
	fromName := strings.TrimSpace(cfg.FromName)
	fromHeader := cfg.From
	if fromName != "" {
		fromHeader = mime.QEncoding.Encode("utf-8", fromName) + " <" + cfg.From + ">"
	}
	b.WriteString("From: " + fromHeader + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64(&b, []byte(body))
	b.WriteString("\r\n")

	if len(attach) > 0 {
		name := attachName
		if name == "" {
			name = "document.pdf"
		}
		b.WriteString("--" + boundary + "\r\n")
		escaped := url.PathEscape(name)
		fallback := "document.pdf"
		if isASCII(name) {
			fallback = name
		}
		b.WriteString("Content-Type: " + contentType + "; name=\"" + fallback + "\"; name*=UTF-8''" + escaped + "\r\n")
		b.WriteString("Content-Disposition: attachment; filename=\"" + fallback + "\"; filename*=UTF-8''" + escaped + "\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeBase64(&b, attach)
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

func writeBase64(b *bytes.Buffer, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
}

func randomBoundary() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "meterzone-" + hex.EncodeToString(buf[:])
}
func isASCII(s string) bool {
	for _, r := range s {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return false
		}
	}
	return true
}

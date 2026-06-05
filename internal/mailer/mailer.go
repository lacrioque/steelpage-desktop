// Package mailer wraps SMTP sending behind a tiny interface. The real
// implementation uses the stdlib net/smtp; when the operator hasn't filled in
// SMTP config we install a no-op so callers don't have to nil-check.
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/markusfluer/steelpage-desktop/internal/config"
)

var (
	ErrNotConfigured = errors.New("mailer not configured")
	ErrSendFailed    = errors.New("send failed")
)

// Mailer is the small surface every caller uses.
type Mailer interface {
	Send(msg Message) error
	Enabled() bool
}

// Live wraps a Mailer and lets the configsvc subscriber swap the underlying
// implementation when SMTP config changes. Callers keep their reference to
// the *Live and always see the latest behaviour.
type Live struct {
	inner Mailer
	mu    sync.RWMutex
}

func NewLive(cfg config.Email) *Live {
	return &Live{inner: New(cfg)}
}

// Reload swaps the underlying mailer based on a fresh config. Safe to call
// concurrently with Send.
func (l *Live) Reload(cfg config.Email) {
	next := New(cfg)
	l.mu.Lock()
	l.inner = next
	l.mu.Unlock()
}

func (l *Live) Send(msg Message) error {
	l.mu.RLock()
	m := l.inner
	l.mu.RUnlock()
	return m.Send(msg)
}

func (l *Live) Enabled() bool {
	l.mu.RLock()
	m := l.inner
	l.mu.RUnlock()
	return m.Enabled()
}

// Message is what callers fill in. HTML is optional; when empty we send
// text/plain only.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// New picks the SMTP implementation when configured, falls back to a no-op
// mailer that just logs. Callers don't need to branch on cfg themselves.
func New(cfg config.Email) Mailer {
	if !cfg.Enabled() {
		return noopMailer{}
	}
	return &smtpMailer{cfg: cfg}
}

type noopMailer struct{}

func (noopMailer) Enabled() bool { return false }
func (noopMailer) Send(msg Message) error {
	log.Printf("mailer: SMTP not configured — DROPPED %q → %s (set email.* in config.yaml to enable)",
		msg.Subject, strings.Join(msg.To, ", "))
	return ErrNotConfigured
}

type smtpMailer struct {
	cfg config.Email
}

func (s *smtpMailer) Enabled() bool { return true }

// Send emits a structured log line on entry, on success, and on each failure
// so operators can grep `mailer:` in journalctl and see exactly which step
// broke. Bodies (and reset/verify links inside them) are deliberately NOT
// logged — only Subject + recipient list.
func (s *smtpMailer) Send(msg Message) error {
	to := strings.Join(msg.To, ", ")
	start := time.Now()
	log.Printf("mailer: sending %q → %s (via %s:%d/%s)",
		msg.Subject, to, s.cfg.Host, s.cfg.Port, s.cfg.Encryption)

	err := s.send(msg)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("mailer: send FAILED %q → %s after %s: %v", msg.Subject, to, dur, err)
		return err
	}
	log.Printf("mailer: sent %q → %s in %s", msg.Subject, to, dur)
	return nil
}

func (s *smtpMailer) send(msg Message) error {
	if len(msg.To) == 0 {
		return fmt.Errorf("%w: no recipients", ErrSendFailed)
	}
	if strings.TrimSpace(msg.Subject) == "" {
		return fmt.Errorf("%w: subject required", ErrSendFailed)
	}

	body, err := buildMIME(s.cfg, msg)
	if err != nil {
		return fmt.Errorf("%w: build mime: %v", ErrSendFailed, err)
	}

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	switch s.cfg.Encryption {
	case "tls":
		return s.sendImplicitTLS(addr, auth, msg.To, body)
	case "starttls":
		return s.sendStartTLS(addr, auth, msg.To, body)
	default:
		return s.sendPlain(addr, auth, msg.To, body)
	}
}

func (s *smtpMailer) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         s.cfg.Host,
		InsecureSkipVerify: s.cfg.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
}

func (s *smtpMailer) sendImplicitTLS(addr string, auth smtp.Auth, to []string, body []byte) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, s.tlsConfig())
	if err != nil {
		return fmt.Errorf("%w: tls dial: %v", ErrSendFailed, err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("%w: smtp client: %v", ErrSendFailed, err)
	}
	defer client.Quit()
	return s.deliver(client, auth, to, body)
}

func (s *smtpMailer) sendStartTLS(addr string, auth smtp.Auth, to []string, body []byte) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("%w: dial: %v", ErrSendFailed, err)
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: smtp client: %v", ErrSendFailed, err)
	}
	defer client.Quit()
	if err := client.StartTLS(s.tlsConfig()); err != nil {
		return fmt.Errorf("%w: starttls: %v", ErrSendFailed, err)
	}
	return s.deliver(client, auth, to, body)
}

func (s *smtpMailer) sendPlain(addr string, auth smtp.Auth, to []string, body []byte) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("%w: dial: %v", ErrSendFailed, err)
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: smtp client: %v", ErrSendFailed, err)
	}
	defer client.Quit()
	return s.deliver(client, auth, to, body)
}

func (s *smtpMailer) deliver(client *smtp.Client, auth smtp.Auth, to []string, body []byte) error {
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("%w: auth: %v", ErrSendFailed, err)
		}
	}
	if err := client.Mail(s.cfg.FromAddress); err != nil {
		return fmt.Errorf("%w: MAIL FROM: %v", ErrSendFailed, err)
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("%w: RCPT TO %q: %v", ErrSendFailed, addr, err)
		}
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("%w: DATA: %v", ErrSendFailed, err)
	}
	if _, err := wc.Write(body); err != nil {
		_ = wc.Close()
		return fmt.Errorf("%w: write body: %v", ErrSendFailed, err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("%w: close body: %v", ErrSendFailed, err)
	}
	return nil
}

package mailer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/markusfluer/steelpage-desktop/internal/config"
)

// buildMIME assembles either a text/plain message or a multipart/alternative
// when both text and html are present. We do this by hand to keep the package
// dependency-free.
func buildMIME(cfg config.Email, msg Message) ([]byte, error) {
	from := formatFrom(cfg)
	headers := []string{
		"From: " + from,
		"To: " + strings.Join(msg.To, ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
	}
	if cfg.ReplyTo != "" {
		headers = append(headers, "Reply-To: "+cfg.ReplyTo)
	}

	var buf bytes.Buffer
	if msg.HTML == "" {
		headers = append(headers, "Content-Type: text/plain; charset=\"utf-8\"")
		headers = append(headers, "Content-Transfer-Encoding: 8bit")
		buf.WriteString(strings.Join(headers, "\r\n"))
		buf.WriteString("\r\n\r\n")
		buf.WriteString(msg.Text)
		return buf.Bytes(), nil
	}

	boundary, err := randomBoundary()
	if err != nil {
		return nil, err
	}
	headers = append(headers, fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q", boundary))
	buf.WriteString(strings.Join(headers, "\r\n"))
	buf.WriteString("\r\n\r\n")

	writePart := func(contentType, body string) {
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: " + contentType + "; charset=\"utf-8\"\r\n")
		buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		buf.WriteString(body)
		buf.WriteString("\r\n")
	}
	writePart("text/plain", msg.Text)
	writePart("text/html", msg.HTML)
	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes(), nil
}

func formatFrom(cfg config.Email) string {
	if cfg.FromName == "" {
		return cfg.FromAddress
	}
	return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", cfg.FromName), cfg.FromAddress)
}

func randomBoundary() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "steelpage_" + hex.EncodeToString(buf), nil
}

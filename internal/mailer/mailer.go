// Package mailer sends transactional email (welcome messages, finished
// reports) over SMTP. Message construction is kept separate from the
// network send so the MIME building can be unit-tested without touching a
// real mail server.
package mailer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"net/smtp"
)

// Mailer sends email via a single configured SMTP account (e.g. a Gmail
// address + app password, as opposed to a normal account password).
type Mailer struct {
	host string
	port string
	from string
	pass string
}

// New builds a Mailer. It does not connect to anything until Send is called.
func New(host, port, from, pass string) *Mailer {
	return &Mailer{host: host, port: port, from: from, pass: pass}
}

func (m *Mailer) addr() string { return m.host + ":" + m.port }

func (m *Mailer) auth() smtp.Auth {
	return smtp.PlainAuth("", m.from, m.pass, m.host)
}

// SendHTML sends a plain HTML email with no attachments — e.g. a welcome message.
func (m *Mailer) SendHTML(to, subject, htmlBody string) error {
	return smtp.SendMail(m.addr(), m.auth(), m.from, []string{to}, buildHTMLMessage(m.from, to, subject, htmlBody))
}

// Attachment is one file to attach to an outgoing email.
type Attachment struct {
	Filename string
	MIMEType string
	Data     []byte
}

// SendWithAttachment sends an HTML email with one attachment (e.g. a PDF report).
func (m *Mailer) SendWithAttachment(to, subject, htmlBody string, att Attachment) error {
	return smtp.SendMail(m.addr(), m.auth(), m.from, []string{to}, buildAttachmentMessage(m.from, to, subject, htmlBody, att))
}

// buildHTMLMessage constructs a raw RFC 5322 message with an HTML body.
// Pure/deterministic (no network) so it can be unit-tested directly.
func buildHTMLMessage(from, to, subject, htmlBody string) []byte {
	var buf bytes.Buffer
	writeHeaders(&buf, from, to, subject)
	buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
	buf.WriteString(htmlBody)
	return buf.Bytes()
}

// buildAttachmentMessage constructs a raw multipart/mixed RFC 5322 message
// with an HTML body plus one base64-encoded attachment.
func buildAttachmentMessage(from, to, subject, htmlBody string, att Attachment) []byte {
	const boundary = "seo-crawler-boundary-8f3a9c2e"

	var buf bytes.Buffer
	writeHeaders(&buf, from, to, subject)
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))

	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
	buf.WriteString(htmlBody)
	buf.WriteString("\r\n")

	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n", att.MIMEType))
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", att.Filename))
	writeBase64Wrapped(&buf, att.Data)

	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return buf.Bytes()
}

func writeHeaders(buf *bytes.Buffer, from, to, subject string) {
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject)))
	buf.WriteString("MIME-Version: 1.0\r\n")
}

// writeBase64Wrapped base64-encodes data and wraps it at 76 characters per
// line, as required by RFC 2045 for message body content.
func writeBase64Wrapped(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
}

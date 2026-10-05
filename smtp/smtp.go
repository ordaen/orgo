// Package smtp sends emails with attachments through an SMTP server.
package smtp

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"github.com/ordaen/orgo/crypt"
)

var (
	// ErrDisabled is returned by Send when sending is not enabled in the settings.
	ErrDisabled = errors.New("smtp: sending is disabled")
	// ErrNoRecipients is returned by Send when the email has no recipients.
	ErrNoRecipients = errors.New("smtp: no recipients")
)

// defaultTimeout is the time a sending attempt may take when the settings have no Timeout.
const defaultTimeout = 60 * time.Second

// tlsPort is the port of SMTP over implicit TLS, the connection is TLS from its start.
const tlsPort = "465"

// New returns new SMTP Agent
func New(c *Settings) *Agent {
	return &Agent{conf: c}
}

// Settings of the SMTP server. On the port 465 the connection is TLS from its start, on the other ports
// STARTTLS is used when the server supports it. The user is authenticated with PLAIN when it is set.
type Settings struct {
	Enabled  bool         `json:"enabled"`
	User     string       `json:"user,omitempty"`
	Password crypt.String `json:"password,omitempty"`
	Host     string       `json:"host,omitempty"`
	Port     string       `json:"port,omitempty"`
	// Retries is the number of retries of a failed sending, the permanent (5xx) server errors are not retried.
	Retries int `json:"retries,string,omitempty"`
	// Delay is the seconds between the retries.
	Delay int `json:"delay,string,omitempty"`
	// Timeout is the seconds a sending attempt may take, 60 when it is not set.
	Timeout int `json:"timeout,string,omitempty"`
}

type ContentType string

const (
	HTML ContentType = "text/html"
	TEXT ContentType = "text/plain"
)

// Agent mailing agent
type Agent struct {
	From        mail.Address
	To          []mail.Address
	CC          []mail.Address
	BCC         []mail.Address
	Subject     string
	Body        []byte
	Attachments []*Attachment
	// ContentType of the body, TEXT when it is empty
	ContentType ContentType

	conf *Settings
}

// Attachment represents an email attachment.
type Attachment struct {
	Filename string
	Data     []byte
}

// SetFrom sets From header
func (a *Agent) SetFrom(name, email string) {
	a.From = mail.Address{Name: name, Address: email}
}

// AddTo adds a recipient to the To header
func (a *Agent) AddTo(name, email string) {
	a.To = append(a.To, mail.Address{Name: name, Address: email})
}

// AddCC adds a recipient to the Cc header
func (a *Agent) AddCC(name, email string) {
	a.CC = append(a.CC, mail.Address{Name: name, Address: email})
}

// AddBCC adds a recipient not shown in the headers
func (a *Agent) AddBCC(name, email string) {
	a.BCC = append(a.BCC, mail.Address{Name: name, Address: email})
}

// Send sends the email to all recipients. A failed sending is retried as configured in the settings.
func (a *Agent) Send() error {
	if a.conf == nil || !a.conf.Enabled {
		return ErrDisabled
	}
	rcpts := a.toAddresses()
	if len(rcpts) == 0 {
		return ErrNoRecipients
	}
	buf := new(bytes.Buffer)
	if err := a.makeMessage(buf); err != nil {
		return err
	}

	delay := time.Duration(max(0, a.conf.Delay)) * time.Second
	for attempt := 0; ; attempt++ {
		err := a.send(rcpts, buf.Bytes())
		if err == nil || attempt >= a.conf.Retries || permanent(err) {
			return err
		}
		time.Sleep(delay)
	}
}

// permanent reports whether err is a permanent SMTP error, which fails again when it is retried.
func permanent(err error) bool {
	var tpErr *textproto.Error
	return errors.As(err, &tpErr) && tpErr.Code >= 500
}

// send sends the message to the recipients in one SMTP session.
func (a *Agent) send(rcpts []string, msg []byte) error {
	timeout := defaultTimeout
	if a.conf.Timeout > 0 {
		timeout = time.Duration(a.conf.Timeout) * time.Second
	}
	addr := net.JoinHostPort(a.conf.Host, a.conf.Port)
	dialer := &net.Dialer{Timeout: timeout}
	tlsConfig := &tls.Config{ServerName: a.conf.Host}
	var conn net.Conn
	var err error
	if a.conf.Port == tlsPort {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp: connect %s: %w", addr, err)
	}
	// the deadline bounds the whole session, also a server that stops responding
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return err
	}

	c, err := smtp.NewClient(conn, a.conf.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if _, isTLS := conn.(*tls.Conn); !isTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
	}
	if a.conf.User != "" {
		auth := smtp.PlainAuth("", a.conf.User, a.conf.Password.Decode(), a.conf.Host)
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(a.From.Address); err != nil {
		return err
	}
	for _, rcpt := range rcpts {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (a *Agent) toAddresses() []string {
	res := make([]string, 0, len(a.CC)+len(a.BCC)+len(a.To))
	for _, list := range [][]mail.Address{a.To, a.CC, a.BCC} {
		for _, addr := range list {
			res = append(res, addr.Address)
		}
	}
	return res
}

// makeMessage writes the email headers and its body, a multipart/mixed body when it has attachments.
func (a *Agent) makeMessage(buf *bytes.Buffer) error {
	for _, addr := range a.allAddresses() {
		if strings.ContainsAny(addr.Address, "\r\n") {
			return fmt.Errorf("smtp: invalid address %q", addr.Address)
		}
	}
	h := &headerWriter{buf: buf}
	h.add("From", a.From.String())
	if len(a.To) > 0 {
		h.add("To", formatAddresses(a.To))
	}
	if len(a.CC) > 0 {
		h.add("Cc", formatAddresses(a.CC))
	}
	// the Q encoding encodes the non-ASCII and control characters, so CR and LF can not add headers
	h.add("Subject", mime.QEncoding.Encode("utf-8", a.Subject))
	h.add("Date", time.Now().Format(time.RFC1123Z))
	h.add("Message-ID", messageID(a.From.Address))
	h.add("MIME-Version", "1.0")

	if len(a.Attachments) == 0 {
		bh := a.bodyHeader()
		h.add("Content-Type", bh.Get("Content-Type"))
		h.add("Content-Transfer-Encoding", bh.Get("Content-Transfer-Encoding"))
		buf.WriteString("\r\n")
		writeBase64(buf, a.Body)
		return nil
	}

	mw := multipart.NewWriter(buf)
	h.add("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mw.Boundary()}))
	buf.WriteString("\r\n")
	part, err := mw.CreatePart(a.bodyHeader())
	if err != nil {
		return err
	}
	writeBase64(part, a.Body)
	for _, at := range a.Attachments {
		part, err := mw.CreatePart(attachmentHeader(at))
		if err != nil {
			return err
		}
		writeBase64(part, at.Data)
	}
	return mw.Close()
}

func (a *Agent) allAddresses() []mail.Address {
	all := append([]mail.Address{a.From}, a.To...)
	all = append(all, a.CC...)
	return append(all, a.BCC...)
}

// bodyHeader returns the headers of the body part.
func (a *Agent) bodyHeader() textproto.MIMEHeader {
	ct := a.ContentType
	if ct == "" {
		ct = TEXT
	}
	return textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(string(ct), map[string]string{"charset": "utf-8"})},
		"Content-Transfer-Encoding": {"base64"},
	}
}

// attachmentHeader returns the headers of the attachment part. A non-ASCII file name is encoded as in RFC 2231.
func attachmentHeader(at *Attachment) textproto.MIMEHeader {
	ct := mime.TypeByExtension(filepath.Ext(at.Filename))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return textproto.MIMEHeader{
		"Content-Type":              {ct},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": at.Filename})},
	}
}

// headerWriter writes header lines.
type headerWriter struct {
	buf *bytes.Buffer
}

func (h *headerWriter) add(key, value string) {
	h.buf.WriteString(key)
	h.buf.WriteString(": ")
	h.buf.WriteString(value)
	h.buf.WriteString("\r\n")
}

func formatAddresses(list []mail.Address) string {
	s := make([]string, len(list))
	for i, addr := range list {
		s[i] = addr.String()
	}
	return strings.Join(s, ", ")
}

// messageID returns a new unique Message-ID in the domain of the from address.
func messageID(from string) string {
	domain := "localhost"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = d
	}
	b := make([]byte, 16)
	rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}

// writeBase64 writes data base64 encoded in lines of 76 characters, as required by RFC 2045.
func writeBase64(w io.Writer, data []byte) {
	const lineLen = 76
	b := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(b, data)
	for len(b) > lineLen {
		w.Write(b[:lineLen])
		w.Write([]byte("\r\n"))
		b = b[lineLen:]
	}
	w.Write(b)
}

// EncodeRFC2047 encodes the name s for an address header
func EncodeRFC2047(s string) string {
	addr := mail.Address{Name: s, Address: ""}
	return strings.Trim(addr.String(), " <>")
}

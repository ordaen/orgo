package smtp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ordaen/orgo/crypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// session is an SMTP session received by the fake server.
type session struct {
	auth  string
	from  string
	rcpts []string
	data  string
}

// serverOptions configure the fake server: it fails the first fails sessions with the reply fail,
// or never responds when stall is set.
type serverOptions struct {
	fail  string
	fails int
	stall bool
}

// fakeServer is an SMTP server on localhost.
type fakeServer struct {
	ln net.Listener
	serverOptions

	mu       sync.Mutex
	sessions []session
	attempts int
}

func newFakeServer(t *testing.T, opts ...serverOptions) *fakeServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &fakeServer{ln: ln}
	if len(opts) > 0 {
		s.serverOptions = opts[0]
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeServer) settings() *Settings {
	_, port, _ := net.SplitHostPort(s.ln.Addr().String())
	return &Settings{Enabled: true, Host: "127.0.0.1", Port: port, Timeout: 5}
}

func (s *fakeServer) Attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func (s *fakeServer) Sessions() []session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]session{}, s.sessions...)
}

func (s *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	s.mu.Lock()
	s.attempts++
	failing := s.attempts <= s.fails
	s.mu.Unlock()
	if s.stall {
		io.Copy(io.Discard, conn)
		return
	}
	tp := textproto.NewConn(conn)
	tp.PrintfLine("220 fake ESMTP")
	var sess session
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(cmd) {
		case "EHLO":
			tp.PrintfLine("250-fake")
			tp.PrintfLine("250 AUTH PLAIN")
		case "AUTH":
			sess.auth = arg
			tp.PrintfLine("235 ok")
		case "MAIL":
			if failing {
				tp.PrintfLine("%s", s.fail)
				continue
			}
			sess.from = arg
			tp.PrintfLine("250 ok")
		case "RCPT":
			sess.rcpts = append(sess.rcpts, arg)
			tp.PrintfLine("250 ok")
		case "DATA":
			tp.PrintfLine("354 go")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			sess.data = string(data)
			s.mu.Lock()
			s.sessions = append(s.sessions, sess)
			s.mu.Unlock()
			tp.PrintfLine("250 ok")
		case "QUIT":
			tp.PrintfLine("221 bye")
			return
		default:
			tp.PrintfLine("250 ok")
		}
	}
}

func newTestAgent(conf *Settings) *Agent {
	a := New(conf)
	a.SetFrom("Victor", "vic@email.com")
	a.AddTo("John", "john@email.com")
	a.Subject = "subject here"
	a.Body = []byte("body here")
	return a
}

// readMessage parses the message written by makeMessage.
func readMessage(t *testing.T, a *Agent) *mail.Message {
	buf := new(bytes.Buffer)
	require.NoError(t, a.makeMessage(buf))
	// RFC 5322 limit, the base64 lines are checked by TestWriteBase64
	for line := range strings.SplitSeq(buf.String(), "\r\n") {
		assert.LessOrEqual(t, len(line), 998, "line length")
	}
	msg, err := mail.ReadMessage(buf)
	require.NoError(t, err)
	return msg
}

func decodeBase64(t *testing.T, r io.Reader) string {
	b, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, r))
	require.NoError(t, err)
	return string(b)
}

func TestToAddresses(t *testing.T) {
	a := Agent{}
	a.AddTo("John", "john@email.com")
	assert.Equal(t, []string{"john@email.com"}, a.toAddresses())
	a.AddCC("", "cc@email.com")
	a.AddBCC("John 1", "john1@email.com")
	assert.Equal(t, []string{"john@email.com", "cc@email.com", "john1@email.com"}, a.toAddresses())
}

func TestMakeMessage(t *testing.T) {
	a := newTestAgent(nil)
	a.AddCC("", "john1@email.com")
	a.AddCC("Jöhn 2", "john2@email.com")
	a.AddBCC("Hidden", "hidden@email.com")
	msg := readMessage(t, a)

	assert.Equal(t, `"Victor" <vic@email.com>`, msg.Header.Get("From"))
	assert.Equal(t, `"John" <john@email.com>`, msg.Header.Get("To"))
	cc, err := msg.Header.AddressList("Cc")
	require.NoError(t, err)
	assert.Equal(t, []*mail.Address{{Address: "john1@email.com"}, {Name: "Jöhn 2", Address: "john2@email.com"}}, cc)
	assert.Empty(t, msg.Header.Get("Bcc"))
	assert.Equal(t, "subject here", msg.Header.Get("Subject"))
	date, err := msg.Header.Date()
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), date, time.Minute)
	assert.Regexp(t, `^<[0-9a-f]{32}@email\.com>$`, msg.Header.Get("Message-ID"))
	assert.Equal(t, `text/plain; charset=utf-8`, msg.Header.Get("Content-Type"))
	assert.Equal(t, "body here", decodeBase64(t, msg.Body), "the body has no trailing boundary")

	a.ContentType = HTML
	msg = readMessage(t, a)
	assert.Equal(t, `text/html; charset=utf-8`, msg.Header.Get("Content-Type"))
}

func TestMakeMessageSubject(t *testing.T) {
	a := newTestAgent(nil)
	a.Subject = "Привет\r\nBcc: injected@email.com"
	msg := readMessage(t, a)
	assert.Empty(t, msg.Header.Get("Bcc"), "CR and LF do not add headers")
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, a.Subject, subject)
}

func TestMakeMessageInvalidAddress(t *testing.T) {
	a := newTestAgent(nil)
	a.AddTo("", "x@email.com\r\nBcc: injected@email.com")
	assert.ErrorContains(t, a.makeMessage(new(bytes.Buffer)), "invalid address")
}

func TestMakeMessageLongBody(t *testing.T) {
	a := newTestAgent(nil)
	a.Body = bytes.Repeat([]byte("long body "), 500)
	msg := readMessage(t, a)
	assert.Equal(t, string(a.Body), decodeBase64(t, msg.Body))
}

func TestMakeMessageAttachments(t *testing.T) {
	a := newTestAgent(nil)
	a.ContentType = HTML
	a.Body = []byte("<p>body</p>")
	a.Attachments = []*Attachment{
		{Filename: "report.pdf", Data: bytes.Repeat([]byte{1, 2, 3}, 100)},
		{Filename: "отчёт без расширения", Data: []byte("data")},
	}
	msg := readMessage(t, a)
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/mixed", mediaType)

	mr := multipart.NewReader(msg.Body, params["boundary"])
	body, err := mr.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "text/html; charset=utf-8", body.Header.Get("Content-Type"))
	assert.Equal(t, "<p>body</p>", decodeBase64(t, body))

	for i, want := range []struct{ name, ct string }{
		{"report.pdf", "application/pdf"},
		{"отчёт без расширения", "application/octet-stream"},
	} {
		part, err := mr.NextPart()
		require.NoError(t, err)
		assert.Equal(t, want.ct, part.Header.Get("Content-Type"))
		assert.Equal(t, want.name, part.FileName())
		assert.Equal(t, string(a.Attachments[i].Data), decodeBase64(t, part))
	}
	_, err = mr.NextPart()
	assert.Equal(t, io.EOF, err)
}

func TestSend(t *testing.T) {
	srv := newFakeServer(t)
	conf := srv.settings()
	conf.User = "user"
	conf.Password = crypt.NewString("secret")
	a := newTestAgent(conf)
	a.AddBCC("", "hidden@email.com")
	require.NoError(t, a.Send())

	sessions := srv.Sessions()
	require.Len(t, sessions, 1)
	s := sessions[0]
	assert.Equal(t, "PLAIN "+base64.StdEncoding.EncodeToString([]byte("\x00user\x00secret")), s.auth)
	assert.Equal(t, "FROM:<vic@email.com>", s.from)
	assert.Equal(t, []string{"TO:<john@email.com>", "TO:<hidden@email.com>"}, s.rcpts)
	msg, err := mail.ReadMessage(bufio.NewReader(strings.NewReader(s.data)))
	require.NoError(t, err)
	assert.Equal(t, "subject here", msg.Header.Get("Subject"))
	assert.Equal(t, "body here", decodeBase64(t, msg.Body))
}

func TestSendWithoutAuth(t *testing.T) {
	srv := newFakeServer(t)
	require.NoError(t, newTestAgent(srv.settings()).Send())
	require.Len(t, srv.Sessions(), 1)
	assert.Empty(t, srv.Sessions()[0].auth)
}

func TestSendDisabled(t *testing.T) {
	srv := newFakeServer(t)
	conf := srv.settings()
	conf.Enabled = false
	assert.ErrorIs(t, newTestAgent(conf).Send(), ErrDisabled)
	assert.ErrorIs(t, newTestAgent(nil).Send(), ErrDisabled)
	assert.Empty(t, srv.Sessions())
}

func TestSendNoRecipients(t *testing.T) {
	srv := newFakeServer(t)
	a := New(srv.settings())
	a.SetFrom("", "vic@email.com")
	assert.ErrorIs(t, a.Send(), ErrNoRecipients)
}

func TestSendRetries(t *testing.T) {
	srv := newFakeServer(t, serverOptions{fail: "421 try again later", fails: 2})
	conf := srv.settings()
	conf.Retries = 2
	require.NoError(t, newTestAgent(conf).Send())
	assert.Len(t, srv.Sessions(), 1)
	assert.Equal(t, 3, srv.Attempts())
}

func TestSendRetriesExhausted(t *testing.T) {
	srv := newFakeServer(t, serverOptions{fail: "421 try again later", fails: 5})
	conf := srv.settings()
	conf.Retries = 1
	assert.ErrorContains(t, newTestAgent(conf).Send(), "try again later")
	assert.Equal(t, 2, srv.Attempts())
}

func TestSendPermanentErrorNotRetried(t *testing.T) {
	srv := newFakeServer(t, serverOptions{fail: "550 mailbox unavailable", fails: 5})
	conf := srv.settings()
	conf.Retries = 3
	assert.ErrorContains(t, newTestAgent(conf).Send(), "mailbox unavailable")
	assert.Equal(t, 1, srv.Attempts())
}

func TestSendTimeout(t *testing.T) {
	srv := newFakeServer(t, serverOptions{stall: true})
	conf := srv.settings()
	conf.Timeout = 1
	start := time.Now()
	assert.Error(t, newTestAgent(conf).Send())
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestSendConnectError(t *testing.T) {
	srv := newFakeServer(t)
	conf := srv.settings()
	srv.ln.Close()
	assert.ErrorContains(t, newTestAgent(conf).Send(), "smtp: connect")
}

func TestWriteBase64(t *testing.T) {
	for _, n := range []int{0, 1, 56, 57, 58, 114, 1000} {
		data := bytes.Repeat([]byte{0xAB}, n)
		buf := new(bytes.Buffer)
		writeBase64(buf, data)
		for line := range strings.SplitSeq(buf.String(), "\r\n") {
			assert.LessOrEqual(t, len(line), 76)
		}
		assert.Equal(t, string(data), decodeBase64(t, strings.NewReader(strings.ReplaceAll(buf.String(), "\r\n", ""))), n)
	}
}

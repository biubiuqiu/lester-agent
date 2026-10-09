package auth

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Mailer interface {
	Send(context.Context, string, string, string) error
}
type SMTPConfig struct {
	Host                              string
	Port                              int
	Username, Password, From, TLSMode string
}
type SMTPMailer struct{ config SMTPConfig }

func NewSMTPMailer(config SMTPConfig) (*SMTPMailer, error) {
	if config.Host == "" || config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("SMTP host and valid port are required")
	}
	address, err := mail.ParseAddress(config.From)
	if err != nil || strings.ContainsAny(config.From, "\r\n") {
		return nil, errors.New("SMTP_FROM must be a valid mailbox")
	}
	config.From = address.String()
	if config.TLSMode != "starttls" && config.TLSMode != "tls" && config.TLSMode != "plain" {
		return nil, errors.New("SMTP_TLS_MODE must be starttls, tls, or plain")
	}
	if config.TLSMode == "plain" && config.Host != "localhost" && config.Host != "127.0.0.1" && config.Host != "::1" {
		return nil, errors.New("plain SMTP is only allowed on localhost")
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, errors.New("SMTP username and password must both be set")
	}
	return &SMTPMailer{config: config}, nil
}
func (m *SMTPMailer) Send(ctx context.Context, to, subject, body string) error {
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(subject, "\r\n") {
		return errors.New("invalid message address or subject")
	}
	from, _ := mail.ParseAddress(m.config.From)
	deadline := time.Now().Add(12 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(m.config.Host, strconv.Itoa(m.config.Port)))
	if err != nil {
		return err
	}
	defer connection.Close()
	if err = connection.SetDeadline(deadline); err != nil {
		return err
	}
	tlsConfig := &tls.Config{ServerName: m.config.Host, MinVersion: tls.VersionTLS12}
	if m.config.TLSMode == "tls" {
		secure := tls.Client(connection, tlsConfig)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		connection = secure
	}
	client, err := smtp.NewClient(connection, m.config.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if m.config.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if m.config.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", m.config.Username, m.config.Password, m.config.Host)); err != nil {
			return err
		}
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(address.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	buffer := bufio.NewWriter(writer)
	_, err = fmt.Fprintf(buffer, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", m.config.From, address.String(), mime.QEncoding.Encode("utf-8", subject), strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	if err == nil {
		err = buffer.Flush()
	}
	closeErr := writer.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return client.Quit()
}
func (s *Service) sendAccountMail(ctx context.Context, email, purpose, token string) error {
	title := "验证你的 Lester 邮箱"
	mode := "verify"
	expiry := "24 小时"
	if purpose == "reset" {
		title = "重设你的 Lester 密码"
		mode = "reset"
		expiry = "30 分钟"
	}
	// Fragments are read by the login page and removed immediately. Tokens never
	// become web-server query strings, referrers, or API access-log fields.
	link := s.options.WebOrigin + "/login?mode=" + mode + "#token=" + token
	return s.options.Mailer.Send(ctx, email, title, "请打开以下链接完成操作：\n\n"+link+"\n\n链接将在 "+expiry+" 后过期，仅能使用一次。如果不是你发起的操作，可以忽略这封邮件。")
}

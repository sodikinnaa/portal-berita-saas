package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	Provider            string // "smtp", "mailgun", "resend", "cloudflare"
	SMTPHost            string
	SMTPPort            string
	SMTPUser            string
	SMTPPass            string
	SMTPEncrypt         string // "tls", "ssl", "none"
	APIKey              string
	CloudflareToken     string
	CloudflareWorkerURL string
}

type Message struct {
	FromEmail string
	FromName  string
	To        string
	Subject   string
	BodyHTML  string
	BodyText  string
}

type Client struct {
	cfg Config
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Send(ctx context.Context, msg Message) error {
	switch c.cfg.Provider {
	case "resend":
		return c.sendResend(ctx, msg)
	case "mailgun":
		return c.sendMailgun(ctx, msg)
	case "cloudflare":
		return c.sendCloudflare(ctx, msg)
	case "direct":
		return c.sendDirect(ctx, msg)
	default:
		return c.sendSMTP(ctx, msg)
	}
}

func (c *Client) sendSMTP(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(c.cfg.SMTPHost, c.cfg.SMTPPort)
	fromHeader := fmt.Sprintf("%s <%s>", msg.FromName, msg.FromEmail)
	if msg.FromName == "" {
		fromHeader = msg.FromEmail
	}

	// Craft raw MIME email message
	boundary := "np_mail_boundary_12345"
	rawMessage := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n"+
		"--%s\r\n"+
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n"+
		"%s\r\n\r\n"+
		"--%s\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n"+
		"%s\r\n\r\n"+
		"--%s--",
		fromHeader, msg.To, msg.Subject, boundary, boundary, msg.BodyText, boundary, msg.BodyHTML, boundary)

	auth := smtp.PlainAuth("", c.cfg.SMTPUser, c.cfg.SMTPPass, c.cfg.SMTPHost)

	// SSL/TLS (Implicit TLS, port 465 usually)
	if c.cfg.SMTPEncrypt == "ssl" {
		tlsconfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         c.cfg.SMTPHost,
		}
		conn, err := tls.Dial("tcp", addr, tlsconfig)
		if err != nil {
			return fmt.Errorf("failed to dial SMTP SSL: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, c.cfg.SMTPHost)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		defer client.Quit()

		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP SSL auth failed: %w", err)
		}

		if err = client.Mail(msg.FromEmail); err != nil {
			return fmt.Errorf("SMTP SSL MAIL FROM failed: %w", err)
		}

		if err = client.Rcpt(msg.To); err != nil {
			return fmt.Errorf("SMTP SSL RCPT TO failed: %w", err)
		}

		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("SMTP SSL DATA failed: %w", err)
		}

		_, err = w.Write([]byte(rawMessage))
		if err != nil {
			return fmt.Errorf("failed to write raw SMTP SSL body: %w", err)
		}

		err = w.Close()
		if err != nil {
			return fmt.Errorf("failed to close SMTP SSL writer: %w", err)
		}
		return nil
	}

	// STARTTLS or None (Plain)
	cDialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := cDialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("failed to initiate SMTP protocol: %w", err)
	}
	defer client.Quit()

	// STARTTLS upgrade if requested
	if c.cfg.SMTPEncrypt == "tls" {
		tlsconfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         c.cfg.SMTPHost,
		}
		if err = client.StartTLS(tlsconfig); err != nil {
			return fmt.Errorf("STARTTLS handshake failed: %w", err)
		}
	}

	if c.cfg.SMTPUser != "" {
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	if err = client.Mail(msg.FromEmail); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}

	if err = client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("SMTP RCPT TO failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}

	_, err = w.Write([]byte(rawMessage))
	if err != nil {
		return fmt.Errorf("failed to write raw SMTP body: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close SMTP writer: %w", err)
	}

	return nil
}

func (c *Client) sendResend(ctx context.Context, msg Message) error {
	type resendPayload struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html"`
		Text    string   `json:"text"`
	}

	fromHeader := fmt.Sprintf("%s <%s>", msg.FromName, msg.FromEmail)
	if msg.FromName == "" {
		fromHeader = msg.FromEmail
	}

	payload := resendPayload{
		From:    fromHeader,
		To:      []string{msg.To},
		Subject: msg.Subject,
		HTML:    msg.BodyHTML,
		Text:    msg.BodyText,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize Resend payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create Resend request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP call to Resend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errorBody)
		return fmt.Errorf("resend API returned status %d: %v", resp.StatusCode, errorBody)
	}

	return nil
}

func (c *Client) sendMailgun(ctx context.Context, msg Message) error {
	// For Mailgun, we can derive the sending domain from FromEmail
	domain := "sandbox"
	parts := strings.Split(msg.FromEmail, "@")
	if len(parts) == 2 {
		domain = parts[1]
	}

	fromHeader := fmt.Sprintf("%s <%s>", msg.FromName, msg.FromEmail)
	if msg.FromName == "" {
		fromHeader = msg.FromEmail
	}

	apiURL := fmt.Sprintf("https://api.mailgun.net/v3/%s/messages", domain)

	// Build form data
	formData := strings.NewReader(fmt.Sprintf(
		"from=%s&to=%s&subject=%s&html=%s&text=%s",
		fromHeader, msg.To, msg.Subject, msg.BodyHTML, msg.BodyText))
	
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, formData)
	if err != nil {
		return fmt.Errorf("failed to create Mailgun request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("api", c.cfg.APIKey)

	httpClient := http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP call to Mailgun: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errorBody)
		return fmt.Errorf("mailgun API returned status %d: %v", resp.StatusCode, errorBody)
	}

	return nil
}

func (c *Client) sendDirect(ctx context.Context, msg Message) error {
	parts := strings.Split(msg.To, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid recipient email address: %s", msg.To)
	}
	domain := parts[1]

	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		return fmt.Errorf("failed to lookup MX records for %s: %w", domain, err)
	}

	var lastErr error
	for _, mx := range mxRecords {
		mxHost := strings.TrimSuffix(mx.Host, ".")
		addr := net.JoinHostPort(mxHost, "25")

		dialer := net.Dialer{Timeout: 10 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			lastErr = err
			continue
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, mxHost)
		if err != nil {
			lastErr = err
			continue
		}
		defer client.Quit()

		senderParts := strings.Split(msg.FromEmail, "@")
		senderDomain := "localhost"
		if len(senderParts) == 2 {
			senderDomain = senderParts[1]
		}
		if err = client.Hello(senderDomain); err != nil {
			lastErr = err
			continue
		}

		if err = client.Mail(msg.FromEmail); err != nil {
			lastErr = err
			continue
		}

		if err = client.Rcpt(msg.To); err != nil {
			lastErr = err
			continue
		}

		w, err := client.Data()
		if err != nil {
			lastErr = err
			continue
		}

		fromHeader := fmt.Sprintf("%s <%s>", msg.FromName, msg.FromEmail)
		if msg.FromName == "" {
			fromHeader = msg.FromEmail
		}

		boundary := "np_mail_boundary_12345"
		rawMessage := fmt.Sprintf("From: %s\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n"+
			"--%s\r\n"+
			"Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n"+
			"%s\r\n\r\n"+
			"--%s\r\n"+
			"Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n"+
			"%s\r\n\r\n"+
			"--%s--",
			fromHeader, msg.To, msg.Subject, boundary, boundary, msg.BodyText, boundary, msg.BodyHTML, boundary)

		_, err = w.Write([]byte(rawMessage))
		if err != nil {
			lastErr = err
			continue
		}

		err = w.Close()
		if err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("failed to send directly via MX servers: %w", lastErr)
	}
	return fmt.Errorf("no MX servers available for %s", domain)
}

func (c *Client) sendCloudflare(ctx context.Context, msg Message) error {
	type cloudflareEmailAddress struct {
		Email string `json:"email"`
		Name  string `json:"name,omitempty"`
	}
	type cloudflarePayload struct {
		To      []cloudflareEmailAddress `json:"to"`
		From    cloudflareEmailAddress   `json:"from"`
		Subject string                   `json:"subject"`
		HTML    string                   `json:"html"`
		Text    string                   `json:"text"`
	}

	targetURL := c.cfg.CloudflareWorkerURL
	if c.cfg.CloudflareToken != "" {
		if strings.Contains(targetURL, "?") {
			targetURL += "&token=" + c.cfg.CloudflareToken
		} else {
			targetURL += "?token=" + c.cfg.CloudflareToken
		}
	}

	payload := cloudflarePayload{
		To: []cloudflareEmailAddress{
			{Email: msg.To},
		},
		From: cloudflareEmailAddress{
			Email: msg.FromEmail,
			Name:  msg.FromName,
		},
		Subject: msg.Subject,
		HTML:    msg.BodyHTML,
		Text:    msg.BodyText,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize Cloudflare payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create Cloudflare proxy request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	httpClient := http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[Cloudflare Send Error] Gagal melakukan request HTTP: %v", err)
		return fmt.Errorf("failed to make HTTP call to Cloudflare Worker: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf("[Cloudflare Worker Response] Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare Worker returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}


package httpserver

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"porta-berita/internal/cms"
	"porta-berita/internal/mail"
)

type dashboardMailViewData struct {
	User          *cms.User
	Settings      map[string]string
	Emails        []cms.Email
	Error         string
	Success       string
	Domain        string
	IsValidDomain bool
	WebhookURL    string
	InboxType     string
}

func generateID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (s *Server) dashboardMailInbox(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	ctx := r.Context()

	// List outbound & inbound emails (Admin sees all emails, other roles see their own)
	var emails []cms.Email
	var err error
	if user.Role == cms.RoleAdmin {
		emails, err = s.store.ListEmails(ctx, "", "", 100, 0)
	} else {
		emails, err = s.store.ListEmails(ctx, user.ID, "", 100, 0)
	}
	if err != nil {
		s.log.Error("failed to list emails", "error", err)
	}

	settings := s.store.GetSettings()
	activeProvider := settings["mail_provider"]

	var filteredEmails []cms.Email
	var inboxType string

	if activeProvider == "cloudflare" {
		inboxType = "Cloudflare"
		for _, e := range emails {
			if strings.Contains(e.Metadata, `"provider":"cloudflare"`) || strings.Contains(e.Metadata, `"provider": "cloudflare"`) {
				filteredEmails = append(filteredEmails, e)
			}
		}
	} else {
		inboxType = "Standard"
		for _, e := range emails {
			if !strings.Contains(e.Metadata, `"provider":"cloudflare"`) && !strings.Contains(e.Metadata, `"provider": "cloudflare"`) {
				filteredEmails = append(filteredEmails, e)
			}
		}
	}

	s.renderTemplate(w, "mail_inbox.html", dashboardMailViewData{
		User:      user,
		Settings:  settings,
		Emails:    filteredEmails,
		InboxType: inboxType,
		Success:   r.URL.Query().Get("success"),
		Error:     r.URL.Query().Get("error"),
	})
}

func (s *Server) dashboardMailCompose(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()

	host := r.Host
	if shost, _, err := net.SplitHostPort(host); err == nil {
		host = shost
	}

	// Check if host is an IP address or localhost
	isIP := net.ParseIP(host) != nil
	isValidDomain := !isIP && host != "localhost" && host != "127.0.0.1" && strings.Contains(host, ".")

	s.renderTemplate(w, "mail_compose.html", dashboardMailViewData{
		User:          user,
		Settings:      settings,
		Error:         r.URL.Query().Get("error"),
		Domain:        host,
		IsValidDomain: isValidDomain,
	})
}

func (s *Server) sendMailHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	recipient := r.FormValue("to")
	subject := r.FormValue("subject")
	body := r.FormValue("body")

	if recipient == "" || subject == "" || body == "" {
		http.Redirect(w, r, "/dashboard/mail/compose?error=Penerima, subjek, dan isi pesan harus diisi", http.StatusSeeOther)
		return
	}

	settings := s.store.GetSettings()
	provider := settings["mail_provider"]

	// Validate SMTP/API credentials are set
	if provider == "" {
		http.Redirect(w, r, "/dashboard/mail/compose?error=Konfigurasi email outbound belum diatur di Pengaturan Situs", http.StatusSeeOther)
		return
	}

	if provider == "smtp" && (settings["mail_smtp_host"] == "" || settings["mail_smtp_port"] == "") {
		http.Redirect(w, r, "/dashboard/mail/compose?error=Host atau port SMTP belum dikonfigurasi", http.StatusSeeOther)
		return
	}

	if (provider == "mailgun" || provider == "resend") && settings["mail_api_key"] == "" {
		http.Redirect(w, r, "/dashboard/mail/compose?error=API Key provider belum dikonfigurasi", http.StatusSeeOther)
		return
	}

	fromEmail := r.FormValue("from_email")
	if fromEmail == "" {
		fromEmail = user.Email
	}
	fromName := r.FormValue("from_name")
	if fromName == "" {
		fromName = user.Name
	}

	// Dynamically resolve domain from request Host
	host := r.Host
	if shost, _, err := net.SplitHostPort(host); err == nil {
		host = shost
	}

	// Check if host is an IP address or localhost
	isIP := net.ParseIP(host) != nil
	if isIP || host == "localhost" || host == "127.0.0.1" {
		s.log.Warn("sending mail from IP or localhost address", "host", host)
	}

	// Force sender domain to match request Host
	parts := strings.Split(fromEmail, "@")
	if len(parts) == 2 {
		fromEmail = parts[0] + "@" + host
	} else {
		fromEmail = "admin@" + host
	}

	// Prepare mail client config
	mailCfg := mail.Config{
		Provider:    provider,
		SMTPHost:    settings["mail_smtp_host"],
		SMTPPort:    settings["mail_smtp_port"],
		SMTPUser:    settings["mail_smtp_user"],
		SMTPPass:    settings["mail_smtp_pass"],
		SMTPEncrypt: settings["mail_smtp_encryption"],
		APIKey:      settings["mail_api_key"],
	}

	if provider == "cloudflare" {
		mailCfg.CloudflareToken = settings["mail_webhook_token"]
		if settings["mail_cloudflare_worker_name"] != "" && settings["mail_cloudflare_subdomain"] != "" {
			mailCfg.CloudflareWorkerURL = fmt.Sprintf("https://%s.%s.workers.dev/send", settings["mail_cloudflare_worker_name"], settings["mail_cloudflare_subdomain"])
		}
	}

	client := mail.NewClient(mailCfg)

	msg := mail.Message{
		FromEmail: fromEmail,
		FromName:  fromName,
		To:        recipient,
		Subject:   subject,
		BodyHTML:  fmt.Sprintf("<div style='font-family: sans-serif; line-height: 1.6;'>%s</div>", strings.ReplaceAll(body, "\n", "<br>")),
		BodyText:  body,
	}

	metadataMap := map[string]string{
		"provider": provider,
	}
	metadataJSON, _ := json.Marshal(metadataMap)

	emailRecord := &cms.Email{
		ID:         generateID(),
		UserID:     &user.ID,
		Direction:  "outbound",
		Sender:     fromEmail,
		SenderName: fromName,
		Recipient:  recipient,
		Subject:    subject,
		BodyHTML:   msg.BodyHTML,
		BodyText:   msg.BodyText,
		CreatedAt:  time.Now(),
		Metadata:   string(metadataJSON),
	}

	// Send email
	sendErr := client.Send(ctx, msg)
	if sendErr != nil {
		emailRecord.Status = "failed"
		emailRecord.ErrorMessage = sendErr.Error()
		s.log.Error("failed to send email", "error", sendErr)

		// Save failed email log to database anyway
		_ = s.store.InsertEmail(ctx, emailRecord)

		http.Redirect(w, r, fmt.Sprintf("/dashboard/mail/compose?error=Gagal mengirim email: %s", sendErr.Error()), http.StatusSeeOther)
		return
	}

	emailRecord.Status = "sent"
	err := s.store.InsertEmail(ctx, emailRecord)
	if err != nil {
		s.log.Error("failed to save sent email record to database", "error", err)
	}

	http.Redirect(w, r, "/dashboard/mail/inbox?success=Email berhasil dikirim!", http.StatusSeeOther)
}

func (s *Server) dashboardMailSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()

	webhookToken := settings["mail_webhook_token"]
	if webhookToken == "" {
		webhookToken = "wb_" + generateID()
		dbSettings := make(map[string]string)
		for k, v := range settings {
			dbSettings[k] = v
		}
		dbSettings["mail_webhook_token"] = webhookToken
		if err := s.store.UpdateSettings(user, dbSettings); err != nil {
			s.log.Error("failed to save generated mail_webhook_token", "error", err)
		}
		settings["mail_webhook_token"] = webhookToken
	}

	webhookScheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		webhookScheme = "https"
	}
	webhookURL := fmt.Sprintf("%s://%s/api/v1/mail/inbound?token=%s", webhookScheme, r.Host, webhookToken)

	s.renderTemplate(w, "mail_settings.html", dashboardMailViewData{
		User:       user,
		Settings:   settings,
		Success:    r.URL.Query().Get("success"),
		Error:      r.URL.Query().Get("error"),
		WebhookURL: webhookURL,
	})
}

func (s *Server) updateMailSettingsHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	existingSettings := s.store.GetSettings()
	settings := make(map[string]string)
	for k, v := range existingSettings {
		settings[k] = v
	}

	settings["mail_provider"] = r.FormValue("mail_provider")
	settings["mail_smtp_host"] = r.FormValue("mail_smtp_host")
	settings["mail_smtp_port"] = r.FormValue("mail_smtp_port")
	settings["mail_smtp_user"] = r.FormValue("mail_smtp_user")
	settings["mail_smtp_pass"] = r.FormValue("mail_smtp_pass")
	settings["mail_smtp_encryption"] = r.FormValue("mail_smtp_encryption")
	settings["mail_api_key"] = r.FormValue("mail_api_key")
	settings["cloudflare_api_token"] = r.FormValue("cloudflare_api_token")
	settings["cloudflare_account_id"] = r.FormValue("cloudflare_account_id")
	if r.FormValue("cloudflare_enable_proxy") == "true" {
		settings["cloudflare_enable_proxy"] = "true"
	} else {
		settings["cloudflare_enable_proxy"] = "false"
	}

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.log.Error("failed to update mail settings", "error", err)
		http.Redirect(w, r, "/dashboard/mail/settings?error=Gagal menyimpan konfigurasi: "+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/mail/settings?success=Konfigurasi email outbound berhasil disimpan!", http.StatusSeeOther)
}


// POST /api/v1/mail/inbound
func (s *Server) apiInboundMailWebhook(w http.ResponseWriter, r *http.Request) {
	// Validate secure webhook token
	settings := s.store.GetSettings()
	expectedToken := settings["mail_webhook_token"]
	if expectedToken != "" {
		token := r.URL.Query().Get("token")
		if token != expectedToken {
			s.log.Warn("unauthorized inbound mail webhook attempt", "token", token)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized: Invalid Token"))
			return
		}
	}

	ctx := r.Context()
	contentType := r.Header.Get("Content-Type")

	type incomingAttachment struct {
		Filename string `json:"filename"`
		MimeType string `json:"mimeType"`
		Content  string `json:"content"` // base64 string
	}
	type resendEmailData struct {
		From        string               `json:"from"`
		To          []string             `json:"to"`
		Subject     string               `json:"subject"`
		HTML        string               `json:"html"`
		Text        string               `json:"text"`
		Attachments []incomingAttachment `json:"attachments"`
	}
	type resendWebhook struct {
		Type string          `json:"type"`
		Data resendEmailData `json:"data"`
	}

	var payload resendWebhook
	var sender, senderName, recipient, subject, bodyHTML, bodyText string
	var rawMetadata []byte

	if strings.Contains(contentType, "application/json") {
		// Parse Resend webhook format
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			s.log.Error("failed to read inbound webhook body", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rawMetadata = bodyBytes

		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			s.log.Error("failed to unmarshal JSON inbound mail", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		sender = payload.Data.From
		if len(payload.Data.To) > 0 {
			recipient = payload.Data.To[0]
		}
		subject = payload.Data.Subject
		bodyHTML = payload.Data.HTML
		bodyText = payload.Data.Text

		// Extract sender name if present in Resend's From string, e.g. "Sodikin <sodikin@beritakita.com>"
		if strings.Contains(sender, "<") && strings.Contains(sender, ">") {
			parts := strings.Split(sender, "<")
			senderName = strings.TrimSpace(parts[0])
			sender = strings.TrimSuffix(strings.TrimSpace(parts[1]), ">")
		}
	} else {
		// Parse Mailgun URL-encoded format
		if err := r.ParseForm(); err != nil {
			s.log.Error("failed to parse url-encoded inbound mail", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		sender = r.FormValue("sender")
		recipient = r.FormValue("recipient")
		subject = r.FormValue("subject")
		bodyHTML = r.FormValue("body-html")
		bodyText = r.FormValue("body-plain")
		
		fromHeader := r.FormValue("from")
		if strings.Contains(fromHeader, "<") && strings.Contains(fromHeader, ">") {
			parts := strings.Split(fromHeader, "<")
			senderName = strings.TrimSpace(parts[0])
		}

		// Save the entire form parameters as JSON metadata
		formMap := make(map[string]string)
		for k, v := range r.Form {
			if len(v) > 0 {
				formMap[k] = v[0]
			}
		}
		rawMetadata, _ = json.Marshal(formMap)
	}

	if recipient == "" || sender == "" {
		s.log.Error("invalid inbound mail: missing recipient or sender")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Route incoming mail to user inbox
	var targetUserID *string
	user, err := s.store.GetUserByEmail(ctx, recipient)
	if err != nil || user == nil {
		s.log.Warn("inbound mail recipient user not found, routing to fallback inbox", "recipient", recipient)
	} else {
		targetUserID = &user.ID
	}

	var savedAttachmentPaths []string
	if len(payload.Data.Attachments) > 0 {
		var ownerUser *cms.User
		if user != nil {
			ownerUser = user
		} else {
			ownerUser, _ = s.store.GetSystemUser()
		}

		if ownerUser != nil {
			_ = os.MkdirAll(s.cfg.UploadDir, 0o755)
			for _, att := range payload.Data.Attachments {
				fileBytes, err := base64.StdEncoding.DecodeString(att.Content)
				if err != nil {
					s.log.Error("failed to decode inbound email attachment base64", "filename", att.Filename, "error", err)
					continue
				}

				ext := filepath.Ext(att.Filename)
				uniqueName := fmt.Sprintf("mail_%d_%s%s", time.Now().UnixNano(), generateID(), ext)
				destPath := filepath.Join(s.cfg.UploadDir, uniqueName)

				if err := os.WriteFile(destPath, fileBytes, 0644); err != nil {
					s.log.Error("failed to save inbound email attachment to disk", "filename", att.Filename, "error", err)
					continue
				}

				// Create Media record
				mediaURL := "/uploads/" + uniqueName
				_, errMedia := s.store.CreateMedia(ownerUser, uniqueName, att.Filename, att.MimeType, mediaURL, int64(len(fileBytes)))
				if errMedia != nil {
					s.log.Error("failed to create media record for inbound email attachment", "filename", att.Filename, "error", errMedia)
				}
				
				savedAttachmentPaths = append(savedAttachmentPaths, mediaURL)
			}
		}
	}

	emailRecord := &cms.Email{
		ID:         generateID(),
		UserID:     targetUserID,
		Direction:  "inbound",
		Sender:     sender,
		SenderName: senderName,
		Recipient:  recipient,
		Subject:    subject,
		BodyHTML:   bodyHTML,
		BodyText:   bodyText,
		Status:     "unread",
		CreatedAt:  time.Now(),
	}

	var metadataMap map[string]any
	if len(rawMetadata) > 0 {
		_ = json.Unmarshal(rawMetadata, &metadataMap)
	}
	if metadataMap == nil {
		metadataMap = make(map[string]any)
	}
	metadataMap["provider"] = "cloudflare"
	if len(savedAttachmentPaths) > 0 {
		metadataMap["attachments"] = savedAttachmentPaths
	}
	finalMetadata, _ := json.Marshal(metadataMap)
	emailRecord.Metadata = string(finalMetadata)

	err = s.store.InsertEmail(ctx, emailRecord)
	if err != nil {
		s.log.Error("failed to save inbound email to database", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.log.Info("successfully routed inbound email", "id", emailRecord.ID, "recipient", recipient)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// POST /dashboard/mail/verify-dns
func (s *Server) verifyDNSHandler(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	domain := settings["custom_domain"]

	var logs []string
	logs = append(logs, fmt.Sprintf("[%s] Memulai pengecekan DNS untuk domain: %s...", time.Now().Format("15:04:05"), domain))

	if domain == "" {
		logs = append(logs, "[ERROR] Domain kustom belum dikonfigurasi di Pengaturan Situs.")
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"logs":    logs,
		})
		return
	}

	spfOK := false
	var spfFound string
	logs = append(logs, fmt.Sprintf("[%s] [SPF] Melakukan lookup TXT untuk %s...", time.Now().Format("15:04:05"), domain))
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[SPF] Lookup gagal: %v", err))
	} else {
		for _, record := range txtRecords {
			if strings.HasPrefix(record, "v=spf1") {
				spfFound = record
				if strings.Contains(record, "newspaper-mail.com") {
					spfOK = true
					logs = append(logs, fmt.Sprintf("[SPF] Sukses: Ditemukan record valid -> %s", record))
				} else {
					logs = append(logs, fmt.Sprintf("[SPF] Warning: Ditemukan record SPF lain -> %s", record))
				}
				break
			}
		}
		if spfFound == "" {
			logs = append(logs, "[SPF] Error: Record SPF (TXT v=spf1) tidak ditemukan.")
		}
	}

	dkimOK := false
	var dkimFound string
	dkimDomain := "newspaper._domainkey." + domain
	logs = append(logs, fmt.Sprintf("[%s] [DKIM] Melakukan lookup TXT untuk %s...", time.Now().Format("15:04:05"), dkimDomain))
	dkimRecords, err := net.LookupTXT(dkimDomain)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[DKIM] Lookup gagal: %v", err))
	} else {
		for _, record := range dkimRecords {
			if strings.HasPrefix(record, "v=DKIM1") {
				dkimFound = record
				if strings.Contains(record, "p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCr6Z9sB8rYqT9z3W4x9h8v7c6b5a4z3y2x1w0v9u8t7s6r5q4p3o2n1m0l9k8j7h6g5f4e3d2c1b0a") {
					dkimOK = true
					logs = append(logs, fmt.Sprintf("[DKIM] Sukses: Ditemukan record DKIM valid -> %s", record))
				} else {
					logs = append(logs, fmt.Sprintf("[DKIM] Error: Kunci publik DKIM tidak cocok -> %s", record))
				}
				break
			}
		}
		if dkimFound == "" {
			logs = append(logs, "[DKIM] Error: Record DKIM (TXT v=DKIM1) tidak ditemukan.")
		}
	}

	mxOK := false
	var mxFound string
	logs = append(logs, fmt.Sprintf("[%s] [MX] Melakukan lookup MX untuk %s...", time.Now().Format("15:04:05"), domain))
	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[MX] Lookup gagal: %v", err))
	} else {
		for _, record := range mxRecords {
			mxFound = record.Host
			if strings.Contains(strings.ToLower(record.Host), "newspaper-mail.com") {
				mxOK = true
				logs = append(logs, fmt.Sprintf("[MX] Sukses: Ditemukan MX record valid -> %s (Priority: %d)", record.Host, 10))
				break
			}
		}
		if !mxOK {
			if len(mxRecords) > 0 {
				logs = append(logs, fmt.Sprintf("[MX] Warning: Ditemukan MX record lain -> %s", mxRecords[0].Host))
			} else {
				logs = append(logs, "[MX] Error: Record MX tidak ditemukan.")
			}
		}
	}

	logs = append(logs, fmt.Sprintf("[%s] Pengecekan DNS selesai.", time.Now().Format("15:04:05")))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"spf_ok":     spfOK,
		"spf_found":  spfFound,
		"dkim_ok":    dkimOK,
		"dkim_found": dkimFound,
		"mx_ok":      mxOK,
		"mx_found":   mxFound,
		"logs":       logs,
	})
}

// POST /dashboard/mail/cloudflare/deploy-worker
func (s *Server) deployCloudflareWorkerHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	settings := s.store.GetSettings()

	// Sanitize host/domain name to form a valid Cloudflare Worker name
	hostName := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		hostName = h
	}
	var sanitizedHost []rune
	for _, rn := range strings.ToLower(hostName) {
		if (rn >= 'a' && rn <= 'z') || (rn >= '0' && rn <= '9') {
			sanitizedHost = append(sanitizedHost, rn)
		} else {
			if len(sanitizedHost) > 0 && sanitizedHost[len(sanitizedHost)-1] != '-' {
				sanitizedHost = append(sanitizedHost, '-')
			}
		}
	}
	sanitizedStr := strings.Trim(string(sanitizedHost), "-")
	workerName := "email-incoming-webhook"
	if sanitizedStr != "" {
		workerName = fmt.Sprintf("email-incoming-webhook-%s", sanitizedStr)
	}

	cfAccountID := strings.TrimSpace(r.FormValue("cf_account_id"))
	if cfAccountID == "" {
		cfAccountID = settings["cloudflare_account_id"]
	}
	cfAPIToken := strings.TrimSpace(r.FormValue("cf_api_token"))
	if cfAPIToken == "" {
		cfAPIToken = settings["cloudflare_api_token"]
	}

	var logs []string
	logs = append(logs, fmt.Sprintf("[%s] Memulai proses unggah script Worker ke Cloudflare...", time.Now().Format("15:04:05")))

	if cfAPIToken == "" {
		logs = append(logs, "[ERROR] API Token wajib diisi lek!")
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "logs": logs})
		return
	}

	cfEnableProxy := r.FormValue("cloudflare_enable_proxy")
	if cfEnableProxy == "" {
		cfEnableProxy = settings["cloudflare_enable_proxy"]
	}

	var client *http.Client
	if cfEnableProxy == "true" {
		proxies := s.store.ListActiveProxies()
		if len(proxies) > 0 {
			p := proxies[mrand.Intn(len(proxies))]
			
			proxyUserMsg := "Tanpa Autentikasi"
			if p.Username != "" {
				proxyUserMsg = p.Username
			}
			logs = append(logs, fmt.Sprintf("[%s] [proxy] configure (Using Proxy: %s:%d User: %s)", time.Now().Format("15:04:05"), p.IP, p.Port, proxyUserMsg))
			
			var baseTransport *http.Transport
			if strings.HasPrefix(p.Protocol, "socks5") {
				var auth *proxy.Auth
				if p.Username != "" && p.Password != "" {
					auth = &proxy.Auth{
						User:     p.Username,
						Password: p.Password,
					}
				}
				dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", p.IP, p.Port), auth, proxy.Direct)
				if err == nil {
					if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
						baseTransport = &http.Transport{
							DialContext: contextDialer.DialContext,
						}
					}
				}
			}
			
			if baseTransport == nil {
				var proxyStr string
				if p.Username != "" && p.Password != "" {
					proxyStr = fmt.Sprintf("%s://%s:%s@%s:%d", p.Protocol, p.Username, p.Password, p.IP, p.Port)
				} else {
					proxyStr = fmt.Sprintf("%s://%s:%d", p.Protocol, p.IP, p.Port)
				}
				if proxyURL, err := url.Parse(proxyStr); err == nil {
					baseTransport = &http.Transport{
						Proxy: http.ProxyURL(proxyURL),
					}
				}
			}
			
			if baseTransport != nil {
				jar, _ := cookiejar.New(nil)
				client = &http.Client{
					Transport: &bandwidthTrackingTransport{
						Transport: baseTransport,
						ProxyID:   p.ID,
						Store:     s.store,
					},
					Timeout: 20 * time.Second,
					Jar:     jar,
				}
				_ = s.store.UpdateProxyLastUsed(p.ID, time.Now().UTC())
			}
		}
		
		if client == nil {
			logs = append(logs, fmt.Sprintf("[%s] [proxy] not configure (Using Direct Connection)", time.Now().Format("15:04:05")))
			client = &http.Client{Timeout: 20 * time.Second}
		}
	} else {
		logs = append(logs, fmt.Sprintf("[%s] [proxy] not configure (Using Direct Connection)", time.Now().Format("15:04:05")))
		client = &http.Client{Timeout: 20 * time.Second}
	}

	// Auto-detect Account ID if not provided
	if cfAccountID == "" {
		logs = append(logs, fmt.Sprintf("[%s] [INFO] Account ID kosong, mencoba mendeteksi Account ID otomatis dari token...", time.Now().Format("15:04:05")))
		accReq, err := http.NewRequestWithContext(ctx, "GET", "https://api.cloudflare.com/client/v4/accounts", nil)
		if err != nil {
			logs = append(logs, fmt.Sprintf("[ERROR] Gagal membuat request deteksi akun: %v", err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
			return
		}
		accReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
		
		accResp, err := client.Do(accReq)
		if err != nil {
			logs = append(logs, fmt.Sprintf("[ERROR] Deteksi akun gagal: %v", err))
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
			return
		}
		defer accResp.Body.Close()
		
		accBody, _ := io.ReadAll(accResp.Body)
		var accResponse struct {
			Success bool `json:"success"`
			Result  []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"result"`
		}
		_ = json.Unmarshal(accBody, &accResponse)
		
		if !accResponse.Success || len(accResponse.Result) == 0 {
			logs = append(logs, "[ERROR] Gagal mendeteksi Account ID secara otomatis. Harap masukkan Account ID Anda secara manual lek!")
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "logs": logs})
			return
		}
		
		cfAccountID = accResponse.Result[0].ID
		logs = append(logs, fmt.Sprintf("[SUKSES] Berhasil mendeteksi Account ID otomatis: %s (%s)", cfAccountID, accResponse.Result[0].Name))
	}

	webhookToken := settings["mail_webhook_token"]
	if webhookToken == "" {
		webhookToken = "wb_" + generateID()
		settings["mail_webhook_token"] = webhookToken
		
		dbSettings := make(map[string]string)
		for k, v := range settings {
			dbSettings[k] = v
		}
		if err := s.store.UpdateSettings(user, dbSettings); err != nil {
			s.log.Error("failed to save generated mail_webhook_token during deploy", "error", err)
		}
	}

	// Generate Webhook URL
	webhookScheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		webhookScheme = "https"
	}
	webhookURL := fmt.Sprintf("%s://%s/api/v1/mail/inbound?token=%s", webhookScheme, r.Host, webhookToken)
	logs = append(logs, fmt.Sprintf("[INFO] Webhook URL terdeteksi: %s", webhookURL))

	// Fetch postal-mime library code
	logs = append(logs, fmt.Sprintf("[%s] Mengunduh dependensi postal-mime dari esm.sh...", time.Now().Format("15:04:05")))
	libResp, err := client.Get("https://esm.sh/postal-mime@2.2.0/es2022/postal-mime.mjs")
	if err != nil || libResp.StatusCode != http.StatusOK {
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal mengunduh library postal-mime: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}
	defer libResp.Body.Close()
	libBytes, err := io.ReadAll(libResp.Body)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal membaca data library postal-mime: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}

	// Generate Worker Code
	workerCode := fmt.Sprintf(`import PostalMime from "./postal-mime.js";

export default {
  async email(message, env, ctx) {
    const rawEmail = new Response(message.raw);
    const arrayBuffer = await rawEmail.arrayBuffer();
    const parser = new PostalMime();
    const email = await parser.parse(arrayBuffer);

    let fromString = message.from;
    if (email.from && email.from.name) {
      fromString = email.from.name + " <" + email.from.address + ">";
    } else if (email.from && email.from.address) {
      fromString = email.from.address;
    }

    const attachments = [];
    if (email.attachments && email.attachments.length > 0) {
      for (const att of email.attachments) {
        const bytes = new Uint8Array(att.content);
        let binary = "";
        const len = bytes.byteLength;
        for (let i = 0; i < len; i++) {
          binary += String.fromCharCode(bytes[i]);
        }
        const base64Content = btoa(binary);
        attachments.push({
          filename: att.filename || "unnamed",
          mimeType: att.mimeType || "application/octet-stream",
          content: base64Content
        });
      }
    }

    const payload = {
      type: "email.received",
      data: {
        from: fromString,
        to: [message.to],
        subject: email.subject || "(No Subject)",
        html: email.html || "",
        text: email.text || "",
        attachments: attachments
      }
    };

    const webhookUrl = "%s";

    try {
      await fetch(webhookUrl, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify(payload)
      });
    } catch (err) {
      console.error("Webhook error: " + err.message);
    }
  },

  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (request.method === "POST" && (url.pathname === "/send" || url.pathname === "/send/")) {
      const token = url.searchParams.get("token");
      const expectedToken = "%s";
      if (token !== expectedToken) {
        return new Response("Unauthorized", { status: 401 });
      }

      try {
        const body = await request.json();
        
        if (!env.SEND_EMAIL) {
          return new Response("Cloudflare send_email binding not configured on this Worker", { status: 500 });
        }

        await env.SEND_EMAIL.send({
          to: body.to,
          from: body.from,
          subject: body.subject,
          text: body.text,
          html: body.html
        });

        return new Response(JSON.stringify({ success: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" }
        });
      } catch (err) {
        return new Response(JSON.stringify({ success: false, error: err.message }), {
          status: 500,
          headers: { "Content-Type": "application/json" }
        });
      }
    }
    return new Response("Not Found", { status: 404 });
  }
}`, webhookURL, webhookToken)

	// Prepare multipart form data body
	var body bytes.Buffer
	wMultipart := multipart.NewWriter(&body)

	// Part 1: metadata
	hMetadata := make(textproto.MIMEHeader)
	hMetadata.Set("Content-Disposition", `form-data; name="metadata"`)
	hMetadata.Set("Content-Type", "application/json")
	pMetadata, err := wMultipart.CreatePart(hMetadata)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal menyusun multipart metadata: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}
	_, _ = pMetadata.Write([]byte(`{"main_module": "index.js", "bindings": [{"type": "send_email", "name": "SEND_EMAIL"}], "observability": {"enabled": true, "head_sampling_rate": 1, "logs": {"enabled": true, "head_sampling_rate": 1, "persist": true, "invocation_logs": true}}}`))

	// Part 2: index.js
	hIndex := make(textproto.MIMEHeader)
	hIndex.Set("Content-Disposition", `form-data; name="index.js"; filename="index.js"`)
	hIndex.Set("Content-Type", "application/javascript+module")
	pIndex, err := wMultipart.CreatePart(hIndex)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal menyusun multipart script: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}
	_, _ = pIndex.Write([]byte(workerCode))

	// Part 3: postal-mime.js
	hLib := make(textproto.MIMEHeader)
	hLib.Set("Content-Disposition", `form-data; name="postal-mime.js"; filename="postal-mime.js"`)
	hLib.Set("Content-Type", "application/javascript+module")
	pLib, err := wMultipart.CreatePart(hLib)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal menyusun multipart library: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}
	_, _ = pLib.Write(libBytes)

	_ = wMultipart.Close()

	logs = append(logs, fmt.Sprintf("[%s] Mengunggah Worker '%s' ke Cloudflare API...", time.Now().Format("15:04:05"), workerName))

	// Send PUT request to Cloudflare
	cfBaseURL := "https://api.cloudflare.com"
	if envBase := os.Getenv("CLOUDFLARE_API_BASE"); envBase != "" {
		cfBaseURL = envBase
	}
	reqUrl := fmt.Sprintf("%s/client/v4/accounts/%s/workers/scripts/%s", cfBaseURL, cfAccountID, workerName)
	cfReq, err := http.NewRequestWithContext(ctx, "PUT", reqUrl, &body)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal membuat HTTP request: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}
	cfReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
	cfReq.Header.Set("Content-Type", wMultipart.FormDataContentType())

	resp, err := client.Do(cfReq)
	if err != nil {
		logs = append(logs, fmt.Sprintf("[ERROR] Hubungan ke Cloudflare API terputus: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "logs": logs})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logs = append(logs, fmt.Sprintf("[ERROR] Cloudflare mengembalikan status %d: %s", resp.StatusCode, string(bodyBytes)))
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "logs": logs})
		return
	}

	var cfResponse struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(bodyBytes, &cfResponse)

	if !cfResponse.Success {
		var errMsg string
		if len(cfResponse.Errors) > 0 {
			errMsg = cfResponse.Errors[0].Message
		}
		logs = append(logs, fmt.Sprintf("[ERROR] Gagal deploy: %s", errMsg))
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "logs": logs})
		return
	}

	// Save credentials to settings
	dbSettings := make(map[string]string)
	for k, v := range settings {
		dbSettings[k] = v
	}
	dbSettings["cloudflare_account_id"] = cfAccountID
	dbSettings["cloudflare_api_token"] = cfAPIToken
	if err := s.store.UpdateSettings(user, dbSettings); err != nil {
		s.log.Error("failed to save cloudflare credentials after deploy success", "error", err)
	}

	logs = append(logs, fmt.Sprintf("[%s] [SUKSES] Worker '%s' berhasil diunggah dan aktif di Cloudflare!", time.Now().Format("15:04:05"), workerName))

	// ── Configure and Enable workers.dev Subdomain ──
	subdomainName := ""
	subReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/client/v4/accounts/%s/workers/subdomain", cfBaseURL, cfAccountID), nil)
	if err == nil {
		subReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
		subRespObj, errSub := client.Do(subReq)
		if errSub == nil {
			defer subRespObj.Body.Close()
			var subResp struct {
				Success bool `json:"success"`
				Result  struct {
					Name string `json:"name"`
				} `json:"result"`
			}
			subBodyBytes, _ := io.ReadAll(subRespObj.Body)
			_ = json.Unmarshal(subBodyBytes, &subResp)
			if subResp.Success && subResp.Result.Name != "" {
				subdomainName = subResp.Result.Name
				logs = append(logs, fmt.Sprintf("[%s] [SUKSES] Menemukan subdomain Workers: %s.workers.dev", time.Now().Format("15:04:05"), subdomainName))
			}
		}
	}

	if subdomainName != "" {
		subBody := `{"enabled":true}`
		enableReq, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/client/v4/accounts/%s/workers/scripts/%s/subdomain", cfBaseURL, cfAccountID, workerName), strings.NewReader(subBody))
		if err == nil {
			enableReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
			enableReq.Header.Set("Content-Type", "application/json")
			enableRespObj, errEnable := client.Do(enableReq)
			if errEnable == nil {
				defer enableRespObj.Body.Close()
				logs = append(logs, fmt.Sprintf("[%s] [SUKSES] Mengaktifkan rute workers.dev untuk Worker '%s'", time.Now().Format("15:04:05"), workerName))
			}
		}

		// Update database settings with subdomain info
		for k, v := range settings {
			dbSettings[k] = v
		}
		dbSettings["mail_cloudflare_subdomain"] = subdomainName
		dbSettings["mail_cloudflare_worker_name"] = workerName
		_ = s.store.UpdateSettings(user, dbSettings)
	}

	// ── Automatically configure Cloudflare Catch-All Routing to route to this Worker ──
	logs = append(logs, fmt.Sprintf("[%s] [INFO] Mendeteksi Zone ID Cloudflare untuk domain '%s'...", time.Now().Format("15:04:05"), hostName))
	
	zoneID := ""
	zoneQueryDomain := hostName
	for {
		zoneReqUrl := fmt.Sprintf("%s/client/v4/zones?name=%s", cfBaseURL, url.QueryEscape(zoneQueryDomain))
		zoneReq, err := http.NewRequestWithContext(ctx, "GET", zoneReqUrl, nil)
		if err != nil {
			break
		}
		zoneReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
		
		zoneResp, err := client.Do(zoneReq)
		if err != nil {
			break
		}
		
		zoneBodyBytes, _ := io.ReadAll(zoneResp.Body)
		zoneResp.Body.Close()
		
		var zoneResponse struct {
			Success bool `json:"success"`
			Result  []struct {
				ID string `json:"id"`
			} `json:"result"`
		}
		_ = json.Unmarshal(zoneBodyBytes, &zoneResponse)
		if zoneResponse.Success && len(zoneResponse.Result) > 0 {
			zoneID = zoneResponse.Result[0].ID
			break
		}
		
		// Fallback: try apex domain if subdomain
		parts := strings.Split(zoneQueryDomain, ".")
		if len(parts) > 2 {
			zoneQueryDomain = strings.Join(parts[len(parts)-2:], ".")
		} else {
			break
		}
	}
	
	if zoneID != "" {
		logs = append(logs, fmt.Sprintf("[SUKSES] Menemukan Zone ID Cloudflare: %s", zoneID))
		logs = append(logs, fmt.Sprintf("[%s] [INFO] Mengonfigurasi Catch-All Email Routing ke Worker '%s'...", time.Now().Format("15:04:05"), workerName))
		
		catchAllUrl := fmt.Sprintf("%s/client/v4/zones/%s/email/routing/rules/catch_all", cfBaseURL, zoneID)
		
		type cfAction struct {
			Type  string   `json:"type"`
			Value []string `json:"value"`
		}
		type catchAllPayload struct {
			Enabled bool       `json:"enabled"`
			Actions []cfAction `json:"actions"`
		}
		
		payload := catchAllPayload{
			Enabled: true,
			Actions: []cfAction{
				{
					Type:  "worker",
					Value: []string{workerName},
				},
			},
		}
		payloadBytes, _ := json.Marshal(payload)
		
		caReq, err := http.NewRequestWithContext(ctx, "PUT", catchAllUrl, bytes.NewReader(payloadBytes))
		if err == nil {
			caReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
			caReq.Header.Set("Content-Type", "application/json")
			
			caResp, err := client.Do(caReq)
			if err == nil {
				caBodyBytes, _ := io.ReadAll(caResp.Body)
				caResp.Body.Close()
				if caResp.StatusCode == http.StatusOK {
					logs = append(logs, fmt.Sprintf("[%s] [SUKSES] Catch-All Email Routing berhasil dikonfigurasi otomatis ke Worker '%s'!", time.Now().Format("15:04:05"), workerName))
				} else {
					logs = append(logs, fmt.Sprintf("[PERINGATAN] Gagal setting Catch-All otomatis (HTTP %d): %s. Silakan setting manual di dashboard Cloudflare.", caResp.StatusCode, string(caBodyBytes)))
				}
			} else {
				logs = append(logs, fmt.Sprintf("[PERINGATAN] Gagal menghubungi API Catch-All Cloudflare: %v", err))
			}
		} else {
			logs = append(logs, fmt.Sprintf("[PERINGATAN] Gagal menyusun request Catch-All: %v", err))
		}
	} else {
		logs = append(logs, fmt.Sprintf("[PERINGATAN] Gagal mendeteksi Zone ID Cloudflare untuk '%s'. Silakan arahkan Catch-all ke Worker '%s' secara manual di dashboard Cloudflare lek!", hostName, workerName))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"logs":    logs,
	})
}

// GET /dashboard/mail/cloudflare/verify-token
func (s *Server) verifyCloudflareTokenHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	settings := s.store.GetSettings()
	cfAPIToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if cfAPIToken == "" {
		cfAPIToken = settings["cloudflare_api_token"]
	}

	if cfAPIToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": false,
		})
		return
	}

	var client *http.Client
	cfEnableProxy := r.URL.Query().Get("enable_proxy")
	if cfEnableProxy == "" {
		cfEnableProxy = settings["cloudflare_enable_proxy"]
	}
	if cfEnableProxy == "true" {
		client = s.getProxyHTTPClient()
	} else {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	// 1. Verify token status
	tokenOK := false
	tokenMsg := "Tidak Aktif"
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.cloudflare.com/client/v4/user/tokens/verify", nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+cfAPIToken)
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			var verifyResponse struct {
				Success bool `json:"success"`
				Result  struct {
					Status string `json:"status"`
				} `json:"result"`
			}
			body, _ := io.ReadAll(resp.Body)
			_ = json.Unmarshal(body, &verifyResponse)
			if verifyResponse.Success && verifyResponse.Result.Status == "active" {
				tokenOK = true
				tokenMsg = "Aktif & Valid"
			}
		}
	}

	if !tokenOK {
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": true,
			"valid":      false,
			"status":     tokenMsg,
		})
		return
	}

	// 2. Fetch accessible Accounts
	var accounts []string
	accReq, err := http.NewRequestWithContext(ctx, "GET", "https://api.cloudflare.com/client/v4/accounts", nil)
	var firstAccountID string
	if err == nil {
		accReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
		resp, err := client.Do(accReq)
		if err == nil {
			defer resp.Body.Close()
			var accResponse struct {
				Success bool `json:"success"`
				Result  []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"result"`
			}
			body, _ := io.ReadAll(resp.Body)
			_ = json.Unmarshal(body, &accResponse)
			if accResponse.Success {
				for _, acc := range accResponse.Result {
					accounts = append(accounts, fmt.Sprintf("%s (%s)", acc.Name, acc.ID))
					if firstAccountID == "" {
						firstAccountID = acc.ID
					}
				}
			}
		}
	}

	// 3. Fetch accessible Zones/Domains
	var zones []string
	var firstZoneID string
	zoneReadOK := false
	zoneReadMsg := "Tidak Ada Akses"
	zoneReq, err := http.NewRequestWithContext(ctx, "GET", "https://api.cloudflare.com/client/v4/zones", nil)
	if err == nil {
		zoneReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
		resp, err := client.Do(zoneReq)
		if err == nil {
			defer resp.Body.Close()
			var zoneResponse struct {
				Success bool `json:"success"`
				Result  []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"result"`
			}
			body, _ := io.ReadAll(resp.Body)
			_ = json.Unmarshal(body, &zoneResponse)
			if zoneResponse.Success {
				zoneReadOK = true
				zoneReadMsg = "Akses Aktif (Read)"
				for _, z := range zoneResponse.Result {
					zones = append(zones, z.Name)
					if firstZoneID == "" {
						firstZoneID = z.ID
					}
				}
			}
		}
	}

	// 4. Test Workers Scripts access on the first account
	workersOK := false
	workersMsg := "Tidak Ada Akses"
	if firstAccountID != "" {
		reqUrl := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts", firstAccountID)
		wReq, err := http.NewRequestWithContext(ctx, "GET", reqUrl, nil)
		if err == nil {
			wReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
			resp, err := client.Do(wReq)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					workersOK = true
					workersMsg = "Akses Aktif (Read/Edit)"
				} else {
					body, _ := io.ReadAll(resp.Body)
					var errResp struct {
						Errors []struct {
							Message string `json:"message"`
						} `json:"errors"`
					}
					_ = json.Unmarshal(body, &errResp)
					if len(errResp.Errors) > 0 {
						workersMsg = fmt.Sprintf("Ditolak: %s", errResp.Errors[0].Message)
					} else {
						workersMsg = fmt.Sprintf("Ditolak (%d)", resp.StatusCode)
					}
				}
			}
		}
	}

	// 5. Test Email Routing Rules access on the first zone
	emailRoutingOK := false
	emailRoutingMsg := "Tidak Ada Akses"
	if firstZoneID != "" {
		reqUrl := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/email/routing/rules", firstZoneID)
		eReq, err := http.NewRequestWithContext(ctx, "GET", reqUrl, nil)
		if err == nil {
			eReq.Header.Set("Authorization", "Bearer "+cfAPIToken)
			resp, err := client.Do(eReq)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					emailRoutingOK = true
					emailRoutingMsg = "Akses Aktif (Read/Edit)"
				} else {
					body, _ := io.ReadAll(resp.Body)
					var errResp struct {
						Errors []struct {
							Message string `json:"message"`
						} `json:"errors"`
					}
					_ = json.Unmarshal(body, &errResp)
					if len(errResp.Errors) > 0 {
						emailRoutingMsg = fmt.Sprintf("Ditolak: %s", errResp.Errors[0].Message)
					} else {
						emailRoutingMsg = fmt.Sprintf("Ditolak (%d)", resp.StatusCode)
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":       true,
		"valid":            true,
		"status":           tokenMsg,
		"accounts":         accounts,
		"zones":            zones,
		"workers":          workersMsg,
		"workers_ok":       workersOK,
		"email_routing":    emailRoutingMsg,
		"email_routing_ok": emailRoutingOK,
		"zone_read":        zoneReadMsg,
		"zone_read_ok":     zoneReadOK,
	})
}


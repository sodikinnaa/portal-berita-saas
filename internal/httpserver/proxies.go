package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	appcms "porta-berita/internal/application/cms"
	"porta-berita/internal/cms"
)

type ProxyAccountGroup struct {
	Username      string
	TotalBytes    int64
	FormattedUsed string
	ProxiesCount  int
	Percentage    float64
}

type dashboardProxiesViewData struct {
	User          *cms.User
	Settings      map[string]string
	Proxies       []cms.Proxy
	Success       string
	Error         string
	AccountGroups []ProxyAccountGroup
	WebshareKeys  []cms.WebshareKey
	FilterStatus  string
	CountAll      int
	CountActive   int
	CountDead     int
	CountChecking int
}

func (s *Server) dashboardProxies(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()
	proxies := s.store.ListProxies()

	var countAll, countActive, countDead, countChecking int
	for _, p := range proxies {
		countAll++
		switch p.Status {
		case "active":
			countActive++
		case "dead":
			countDead++
		default:
			countChecking++
		}
	}

	filterStatus := r.URL.Query().Get("status")
	if filterStatus != "" {
		var filtered []cms.Proxy
		for _, p := range proxies {
			if p.Status == filterStatus || (filterStatus == "checking" && p.Status != "active" && p.Status != "dead") {
				filtered = append(filtered, p)
			}
		}
		proxies = filtered
	}

	// Group proxies by username
	groupsMap := make(map[string]*ProxyAccountGroup)
	for _, p := range proxies {
		username := p.Username
		if username == "" {
			username = "Tanpa Autentikasi"
		}
		
		totalBytes := p.BytesSent + p.BytesReceived
		
		g, exists := groupsMap[username]
		if !exists {
			g = &ProxyAccountGroup{
				Username: username,
			}
			groupsMap[username] = g
		}
		
		g.TotalBytes += totalBytes
		g.ProxiesCount++
	}

	var accountGroups []ProxyAccountGroup
	for _, g := range groupsMap {
		g.FormattedUsed = formatBytes(g.TotalBytes)
		
		// 1 GB quota limit in bytes (1024 * 1024 * 1024)
		const quotaBytes int64 = 1024 * 1024 * 1024 
		if g.Username != "Tanpa Autentikasi" {
			g.Percentage = (float64(g.TotalBytes) / float64(quotaBytes)) * 100
			if g.Percentage > 100 {
				g.Percentage = 100
			}
		} else {
			g.Percentage = 0
		}
		
		accountGroups = append(accountGroups, *g)
	}

	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")
	webshareKeys, _ := s.store.ListWebshareKeys()

	// Automatically query Webshare API on page load to display real-time synced bandwidth usage
	client := &http.Client{Timeout: 3 * time.Second}
	for i, k := range webshareKeys {
		statsURL := "https://proxy.webshare.io/api/v2/stats/aggregate/"
		reqStats, err := http.NewRequest("GET", statsURL, nil)
		if err == nil {
			reqStats.Header.Set("Authorization", "Token "+k.APIKey)
			respStats, err := client.Do(reqStats)
			if err == nil && respStats.StatusCode == http.StatusOK {
				var statsData struct {
					BandwidthTotal int64 `json:"bandwidth_total"`
				}
				if err := json.NewDecoder(respStats.Body).Decode(&statsData); err == nil {
					_ = s.store.UpdateWebshareKeyBandwidth(k.ID, statsData.BandwidthTotal)
					webshareKeys[i].BytesUsed = statsData.BandwidthTotal
				}
				respStats.Body.Close()
			}
		}
	}

	s.renderTemplate(w, "proxies.html", dashboardProxiesViewData{
		User:          user,
		Settings:      settings,
		Proxies:       proxies,
		Success:       successMsg,
		Error:         errorMsg,
		AccountGroups: accountGroups,
		WebshareKeys:  webshareKeys,
		FilterStatus:  filterStatus,
		CountAll:      countAll,
		CountActive:   countActive,
		CountDead:     countDead,
		CountChecking: countChecking,
	})
}

func (s *Server) proxyCreate(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	ip := strings.TrimSpace(r.FormValue("ip"))
	portStr := strings.TrimSpace(r.FormValue("port"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	protocol := strings.TrimSpace(r.FormValue("protocol"))

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		s.renderTemplate(w, "proxies.html", dashboardProxiesViewData{
			User:     user,
			Settings: s.store.GetSettings(),
			Proxies:  s.store.ListProxies(),
			Error:    "Port harus berupa angka positif",
		})
		return
	}

	_, err = s.store.CreateProxy(user, cms.ProxyInput{
		IP:       ip,
		Port:     port,
		Username: username,
		Password: password,
		Protocol: protocol,
	})

	if err != nil {
		s.renderTemplate(w, "proxies.html", dashboardProxiesViewData{
			User:     user,
			Settings: s.store.GetSettings(),
			Proxies:  s.store.ListProxies(),
			Error:    "Gagal menambahkan proxy: " + err.Error(),
		})
		return
	}

	http.Redirect(w, r, "/dashboard/proxies", http.StatusSeeOther)
}

func (s *Server) proxyBatchImport(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	protocol := strings.TrimSpace(r.FormValue("protocol"))
	rawText := r.FormValue("proxies")

	lines := strings.Split(rawText, "\n")
	successCount := 0
	errCount := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			errCount++
			continue
		}

		ip := strings.TrimSpace(parts[0])
		portStr := strings.TrimSpace(parts[1])
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			errCount++
			continue
		}

		var username, password string
		if len(parts) >= 3 {
			username = strings.TrimSpace(parts[2])
		}
		if len(parts) >= 4 {
			password = strings.TrimSpace(parts[3])
		}

		p, err := s.store.CreateProxy(user, cms.ProxyInput{
			IP:       ip,
			Port:     port,
			Username: username,
			Password: password,
			Protocol: protocol,
		})

		if err != nil {
			errCount++
			continue
		}

		successCount++

		// Trigger check asynchronously
		go func(pCopy cms.Proxy) {
			latency, err := CheckProxyConnection(pCopy)
			if err != nil {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "dead", 0)
			} else {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "active", latency)
			}
		}(*p)
	}

	successMsg := fmt.Sprintf("Berhasil mengimpor %d proxy (gagal: %d). Pemeriksaan status otomatis telah dimulai.", successCount, errCount)
	http.Redirect(w, r, "/dashboard/proxies?success="+url.QueryEscape(successMsg), http.StatusSeeOther)
}

type webshareProxyItem struct {
	ProxyAddress string `json:"proxy_address"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Valid        bool   `json:"valid"`
}

type webshareListResponse struct {
	Count   int                  `json:"count"`
	Results []webshareProxyItem  `json:"results"`
}

func (s *Server) syncWebshareKeys(user *cms.User, apiKeys []string, protocol string) (int, int, int, error) {
	successCount := 0
	skipCount := 0
	errCount := 0

	// Fetch existing proxies to avoid duplicates (distinguished by IP, Port, Username, and Password)
	existingProxies := s.store.ListProxies()
	existingMap := make(map[string]bool)
	for _, ep := range existingProxies {
		key := fmt.Sprintf("%s:%d:%s:%s", ep.IP, ep.Port, ep.Username, ep.Password)
		existingMap[key] = true
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, apiKey := range apiKeys {
		// Fetch actual bandwidth from Webshare API stats endpoint
		statsURL := "https://proxy.webshare.io/api/v2/stats/aggregate/"
		reqStats, err := http.NewRequest("GET", statsURL, nil)
		if err == nil {
			reqStats.Header.Set("Authorization", "Token "+apiKey)
			respStats, err := client.Do(reqStats)
			if err == nil && respStats.StatusCode == http.StatusOK {
				var statsData struct {
					BandwidthTotal int64 `json:"bandwidth_total"`
				}
				if err := json.NewDecoder(respStats.Body).Decode(&statsData); err == nil {
					// Update saved key bandwidth in database
					savedKeys, err := s.store.ListWebshareKeys()
					if err == nil {
						for _, k := range savedKeys {
							if k.APIKey == apiKey {
								_ = s.store.UpdateWebshareKeyBandwidth(k.ID, statsData.BandwidthTotal)
								break
							}
						}
					}
				}
				respStats.Body.Close()
			}
		}

		// Make request to Webshare API v2 to get proxy list
		reqURL := "https://proxy.webshare.io/api/v2/proxy/list/?mode=direct&page_size=100"
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			errCount++
			continue
		}
		req.Header.Set("Authorization", "Token "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			errCount++
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			errCount++
			continue
		}

		var webshareResp webshareListResponse
		if err := json.NewDecoder(resp.Body).Decode(&webshareResp); err != nil {
			resp.Body.Close()
			errCount++
			continue
		}
		resp.Body.Close()

		for _, item := range webshareResp.Results {
			key := fmt.Sprintf("%s:%d:%s:%s", item.ProxyAddress, item.Port, item.Username, item.Password)
			if existingMap[key] {
				skipCount++
				continue
			}

			p, err := s.store.CreateProxy(user, cms.ProxyInput{
				IP:       item.ProxyAddress,
				Port:     item.Port,
				Username: item.Username,
				Password: item.Password,
				Protocol: protocol,
			})

			if err != nil {
				errCount++
				continue
			}

			existingMap[key] = true
			successCount++

			// Trigger check asynchronously
			go func(pCopy cms.Proxy) {
				latency, err := CheckProxyConnection(pCopy)
				if err != nil {
					_ = s.store.UpdateProxyStatus(pCopy.ID, "dead", 0)
				} else {
					_ = s.store.UpdateProxyStatus(pCopy.ID, "active", latency)
				}
			}(*p)
		}
	}

	return successCount, skipCount, errCount, nil
}

func (s *Server) proxyWebshareSync(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	protocol := strings.TrimSpace(r.FormValue("protocol"))
	if protocol == "" {
		protocol = "http"
	}

	// Get API Key(s) to sync
	var apiKeys []string
	customAPIKey := strings.TrimSpace(r.FormValue("api_key"))

	if customAPIKey != "" {
		apiKeys = append(apiKeys, customAPIKey)
	} else {
		// Sync all saved keys
		savedKeys, err := s.store.ListWebshareKeys()
		if err != nil {
			http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("Gagal memuat API Key tersimpan: "+err.Error()), http.StatusSeeOther)
			return
		}
		for _, k := range savedKeys {
			apiKeys = append(apiKeys, k.APIKey)
		}
	}

	if len(apiKeys) == 0 {
		http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("Tidak ada API Key yang dimasukkan atau disimpan untuk disinkronisasi."), http.StatusSeeOther)
		return
	}

	successCount, skipCount, errCount, err := s.syncWebshareKeys(user, apiKeys, protocol)
	if err != nil {
		http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("Gagal menyinkronkan proxy: "+err.Error()), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Sinkronisasi Webshare sukses! Berhasil mengimpor %d proxy baru dari %d akun (dilewati: %d, gagal: %d).", successCount, len(apiKeys), skipCount, errCount)
	http.Redirect(w, r, "/dashboard/proxies?success="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) webshareKeyAdd(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	label := strings.TrimSpace(r.FormValue("label"))
	if apiKey == "" || label == "" {
		http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("API Key dan Label tidak boleh kosong"), http.StatusSeeOther)
		return
	}

	_, err := s.store.AddWebshareKey(user, apiKey, label)
	if err != nil {
		http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("Gagal menyimpan API Key: "+err.Error()), http.StatusSeeOther)
		return
	}

	// Automatically trigger sync for the new key! Default to SOCKS5 as standard proxy type
	successCount, skipCount, errCount, _ := s.syncWebshareKeys(user, []string{apiKey}, "socks5")

	msg := fmt.Sprintf("Berhasil menyimpan API Key Webshare baru dan otomatis menyinkronkan %d proxy (dilewati: %d, gagal: %d)!", successCount, skipCount, errCount)
	http.Redirect(w, r, "/dashboard/proxies?success="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) webshareKeyDelete(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	id := r.PathValue("id")

	err := s.store.DeleteWebshareKey(user, id)
	if err != nil {
		http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("Gagal menghapus API Key: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/proxies?success="+url.QueryEscape("Berhasil menghapus API Key Webshare!"), http.StatusSeeOther)
}

func (s *Server) proxyDelete(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	id := r.PathValue("id")

	err := s.store.DeleteProxy(user, id)
	if err != nil {
		s.renderTemplate(w, "proxies.html", dashboardProxiesViewData{
			User:     user,
			Settings: s.store.GetSettings(),
			Proxies:  s.store.ListProxies(),
			Error:    "Gagal menghapus proxy: " + err.Error(),
		})
		return
	}

	http.Redirect(w, r, "/dashboard/proxies", http.StatusSeeOther)
}

func (s *Server) proxyCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	p, err := s.store.GetProxyByID(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Proxy tidak ditemukan"})
		return
	}

	_ = s.store.UpdateProxyStatus(id, "checking", 0)

	latency, err := CheckProxyConnection(*p)
	if err != nil {
		s.log.Error("Proxy check failed", "id", id, "ip", p.IP, "error", err)
		_ = s.store.UpdateProxyStatus(id, "dead", 0)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
			"ip":      p.IP,
			"port":    p.Port,
		})
		return
	}

	s.log.Info("Proxy check success", "id", id, "ip", p.IP, "latency", latency)
	_ = s.store.UpdateProxyStatus(id, "active", latency)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"latency": latency,
		"ip":      p.IP,
		"port":    p.Port,
	})
}

func (s *Server) proxyCheckAll(w http.ResponseWriter, r *http.Request) {
	proxies := s.store.ListProxies()

	for _, p := range proxies {
		pCopy := p
		_ = s.store.UpdateProxyStatus(pCopy.ID, "checking", 0)
		go func() {
			latency, err := CheckProxyConnection(pCopy)
			if err != nil {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "dead", 0)
			} else {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "active", latency)
			}
		}()
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Pemeriksaan semua proxy dimulai"})
}

func CheckProxyConnection(p cms.Proxy) (int, error) {
	var proxyStr string
	if p.Username != "" && p.Password != "" {
		proxyStr = fmt.Sprintf("%s://%s:%s@%s:%d", p.Protocol, p.Username, p.Password, p.IP, p.Port)
	} else {
		proxyStr = fmt.Sprintf("%s://%s:%d", p.Protocol, p.IP, p.Port)
	}

	var transport *http.Transport
	if strings.HasPrefix(p.Protocol, "socks5") {
		var auth *proxy.Auth
		if p.Username != "" && p.Password != "" {
			auth = &proxy.Auth{
				User:     p.Username,
				Password: p.Password,
			}
		}
		dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", p.IP, p.Port), auth, proxy.Direct)
		if err != nil {
			return 0, err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return 0, fmt.Errorf("socks5 dialer does not support ContextDialer")
		}
		transport = &http.Transport{
			DialContext: contextDialer.DialContext,
		}
	} else {
		proxyURL, err := url.Parse(proxyStr)
		if err != nil {
			return 0, err
		}
		transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	start := time.Now()
	// Test against Google News robots.txt or reliable endpoint
	resp, err := client.Get("https://news.google.com/robots.txt")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return 0, fmt.Errorf("returned HTTP status %d", resp.StatusCode)
	}

	return int(time.Since(start).Milliseconds()), nil
}

func (s *Server) getProxyHTTPClient() *http.Client {
	proxies := s.store.ListActiveProxies()
	
	// Create cookie jar for Google News redirects and sessions
	jar, _ := cookiejar.New(nil)

	if len(proxies) == 0 {
		s.log.Info("proxy not configure")
		// Fallback to direct client
		return &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
		}
	}

	// Pick a random proxy from active proxies
	p := proxies[rand.Intn(len(proxies))]

	var proxyStr string
	if p.Username != "" && p.Password != "" {
		proxyStr = fmt.Sprintf("%s://%s:%s@%s:%d", p.Protocol, p.Username, p.Password, p.IP, p.Port)
	} else {
		proxyStr = fmt.Sprintf("%s://%s:%d", p.Protocol, p.IP, p.Port)
	}

	s.log.Info("use proxy", "proxy", p.IP, "port", p.Port, "protocol", p.Protocol)

	// Update last used timestamp
	_ = s.store.UpdateProxyLastUsed(p.ID, time.Now().UTC())

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
		if err != nil {
			s.log.Error("failed to create socks5 dialer, falling back to direct", "error", err)
			return &http.Client{
				Timeout: 15 * time.Second,
				Jar:     jar,
			}
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			s.log.Error("socks5 dialer does not support ContextDialer, falling back to direct")
			return &http.Client{
				Timeout: 15 * time.Second,
				Jar:     jar,
			}
		}
		baseTransport = &http.Transport{
			DialContext: contextDialer.DialContext,
		}
	} else {
		proxyURL, err := url.Parse(proxyStr)
		if err != nil {
			s.log.Error("failed to parse proxy URL, falling back to direct", "url", proxyStr, "error", err)
			return &http.Client{
				Timeout: 15 * time.Second,
				Jar:     jar,
			}
		}
		baseTransport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	return &http.Client{
		Transport: &bandwidthTrackingTransport{
			Transport: baseTransport,
			ProxyID:   p.ID,
			Store:     s.store,
		},
		Timeout: 15 * time.Second,
		Jar:     jar,
	}
}

type selfHealingTransport struct {
	server  *Server
	timeout time.Duration
}

func (s *Server) getProxyHTTPClientWithTimeout(timeout time.Duration) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Transport: &selfHealingTransport{
			server:  s,
			timeout: timeout,
		},
		Timeout: timeout,
		Jar:     jar,
	}
}

func (t *selfHealingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Proactively bypass proxy rotator for custom/private AI endpoints (like member.wakdondin.my.id)
	// because host firewalls (Hostinger/LiteSpeed) block public proxy IPs.
	bypassProxy := false
	if req.URL.Host == "member.wakdondin.my.id" || strings.Contains(req.URL.Host, "wakdondin") {
		bypassProxy = true
	} else {
		settings := t.server.store.GetSettings()
		if ep, ok := settings["ai_endpoint_url"]; ok && ep != "" {
			if epURL, err := url.Parse(ep); err == nil && epURL.Host != "" {
				if req.URL.Host == epURL.Host {
					bypassProxy = true
				}
			}
		}
	}

	if bypassProxy {
		t.server.log.Info("Proactively bypassing proxy rotator for custom/private AI endpoint to prevent firewall blocks", "host", req.URL.Host)
		transport := &http.Transport{}
		return transport.RoundTrip(req)
	}

	proxies := t.server.store.ListActiveProxies()
	if len(proxies) == 0 {
		t.server.log.Info("no active proxies configured, using direct connection")
		transport := &http.Transport{}
		return transport.RoundTrip(req)
	}

	maxAttempts := 3
	if len(proxies) < maxAttempts {
		maxAttempts = len(proxies)
	}

	// Shuffle proxies to avoid hitting the same failed proxy first on subsequent requests
	shuffled := make([]cms.Proxy, len(proxies))
	copy(shuffled, proxies)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		p := shuffled[i]

		var proxyStr string
		if p.Username != "" && p.Password != "" {
			proxyStr = fmt.Sprintf("%s://%s:%s@%s:%d", p.Protocol, p.Username, p.Password, p.IP, p.Port)
		} else {
			proxyStr = fmt.Sprintf("%s://%s:%d", p.Protocol, p.IP, p.Port)
		}

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
			if err != nil {
				lastErr = err
				continue
			}
			contextDialer, ok := dialer.(proxy.ContextDialer)
			if !ok {
				lastErr = fmt.Errorf("socks5 dialer does not support ContextDialer")
				continue
			}
			baseTransport = &http.Transport{
				DialContext: contextDialer.DialContext,
			}
		} else {
			proxyURL, err := url.Parse(proxyStr)
			if err != nil {
				lastErr = err
				continue
			}
			baseTransport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}

		t.server.log.Info("attempting request with proxy", "proxy", p.IP, "port", p.Port, "attempt", i+1)
		_ = t.server.store.UpdateProxyLastUsed(p.ID, time.Now().UTC())

		trackingTransport := &bandwidthTrackingTransport{
			Transport: baseTransport,
			ProxyID:   p.ID,
			Store:     t.server.store,
		}

		var reqClone *http.Request
		if req.Body != nil {
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			req.Body.Close()

			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			reqClone = req.Clone(req.Context())
			reqClone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else {
			reqClone = req.Clone(req.Context())
		}

		attemptCtx, cancelAttempt := context.WithTimeout(reqClone.Context(), 15*time.Second)
		reqClone = reqClone.WithContext(attemptCtx)

		resp, err := trackingTransport.RoundTrip(reqClone)
		cancelAttempt()
		
		logType := "Asisten AI"
		if strings.Contains(req.URL.Path, "models") {
			logType = "Tarik Model"
		} else if strings.Contains(req.URL.Path, "generateContent") || strings.Contains(req.URL.Path, "chat/completions") {
			logType = "Generate Content"
		}

		if err == nil {
			t.server.addAILog(logType, req.URL.Host, fmt.Sprintf("%s:%d", p.IP, p.Port), "Success", "Koneksi sukses")
			return resp, nil
		}

		t.server.log.Warn("proxy request failed, trying next proxy", "proxy", p.IP, "port", p.Port, "error", err)
		t.server.addAILog(logType, req.URL.Host, fmt.Sprintf("%s:%d", p.IP, p.Port), "Failed", err.Error())
		lastErr = err
	}

	logType := "Asisten AI"
	if strings.Contains(req.URL.Path, "models") {
		logType = "Tarik Model"
	}
	t.server.addAILog(logType, req.URL.Host, "None/All Attempts Failed", "Failed", fmt.Sprintf("Semua %d proxy gagal dicoba. Error terakhir: %s", maxAttempts, lastErr.Error()))
	return nil, fmt.Errorf("all %d proxy attempts failed. Last error: %w", maxAttempts, lastErr)
}

type bandwidthTrackingTransport struct {
	Transport http.RoundTripper
	ProxyID   string
	Store     appcms.ContentStore
}

func (t *bandwidthTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBytes int64
	if req.URL != nil {
		reqBytes += int64(len(req.URL.String()))
	}
	reqBytes += int64(len(req.Method))
	for k, vv := range req.Header {
		reqBytes += int64(len(k))
		for _, v := range vv {
			reqBytes += int64(len(v))
		}
	}
	if req.ContentLength > 0 {
		reqBytes += req.ContentLength
	}

	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		_ = t.Store.AddProxyBandwidth(t.ProxyID, reqBytes, 0)
		return nil, err
	}

	var respBytes int64
	for k, vv := range resp.Header {
		respBytes += int64(len(k))
		for _, v := range vv {
			respBytes += int64(len(v))
		}
	}

	resp.Body = &countingReadCloser{
		ReadCloser: resp.Body,
		OnClose: func(bytesRead int64) {
			_ = t.Store.AddProxyBandwidth(t.ProxyID, reqBytes, respBytes+bytesRead)
		},
	}

	return resp, nil
}

type countingReadCloser struct {
	io.ReadCloser
	bytesRead int64
	OnClose   func(bytesRead int64)
}

func (c *countingReadCloser) Read(p []byte) (n int, err error) {
	n, err = c.ReadCloser.Read(p)
	c.bytesRead += int64(n)
	return n, err
}

func (c *countingReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.OnClose != nil {
		c.OnClose(c.bytesRead)
		c.OnClose = nil
	}
	return err
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// POST /dashboard/proxies/scrape-public
func (s *Server) proxyScrapePublic(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	protocol := strings.TrimSpace(r.FormValue("protocol"))
	if protocol == "" {
		protocol = "http"
	}

	var urls []string
	if protocol == "socks5" {
		urls = []string{
			"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&timeout=5000&country=all&ssl=all&anonymity=all",
			"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/socks5.txt",
		}
	} else {
		protocol = "http"
		urls = []string{
			"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=5000&country=all&ssl=all&anonymity=all",
			"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
		}
	}

	var rawProxies []string
	client := &http.Client{Timeout: 8 * time.Second}

	for _, apiURL := range urls {
		resp, err := client.Get(apiURL)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				rawProxies = append(rawProxies, line)
			}
		}
		resp.Body.Close()
	}

	if len(rawProxies) == 0 {
		http.Redirect(w, r, "/dashboard/proxies?error="+url.QueryEscape("Tidak ada proxy publik yang ditemukan dari server sumber."), http.StatusSeeOther)
		return
	}

	existingProxies := s.store.ListProxies()
	existingMap := make(map[string]bool)
	for _, ep := range existingProxies {
		key := fmt.Sprintf("%s:%d", ep.IP, ep.Port)
		existingMap[key] = true
	}

	successCount := 0
	skipCount := 0
	errCount := 0
	maxNewImports := 100

	for _, raw := range rawProxies {
		if successCount >= maxNewImports {
			break
		}

		parts := strings.Split(strings.TrimSpace(raw), ":")
		if len(parts) < 2 {
			continue
		}

		ip := strings.TrimSpace(parts[0])
		portStr := strings.TrimSpace(parts[1])
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			continue
		}

		key := fmt.Sprintf("%s:%d", ip, port)
		if existingMap[key] {
			skipCount++
			continue
		}

		p, err := s.store.CreateProxy(user, cms.ProxyInput{
			IP:       ip,
			Port:     port,
			Protocol: protocol,
		})

		if err != nil {
			errCount++
			continue
		}

		existingMap[key] = true
		successCount++

		// Trigger check asynchronously
		go func(pCopy cms.Proxy) {
			latency, err := CheckProxyConnection(pCopy)
			if err != nil {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "dead", 0)
			} else {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "active", latency)
			}
		}(*p)
	}

	msg := fmt.Sprintf("Scraping proxy publik sukses! Berhasil mengimpor %d proxy %s baru (dilewati: %d, gagal: %d). Pemeriksaan status otomatis telah dimulai.", successCount, strings.ToUpper(protocol), skipCount, errCount)
	http.Redirect(w, r, "/dashboard/proxies?success="+url.QueryEscape(msg), http.StatusSeeOther)
}

type dashboardProxyScraperViewData struct {
	User     *cms.User
	Settings map[string]string
	Success  string
	Error    string
}

// GET /dashboard/proxies/scraper
func (s *Server) dashboardProxyScraper(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	s.renderTemplate(w, "proxy_scraper.html", dashboardProxyScraperViewData{
		User:     user,
		Settings: settings,
		Success:  successMsg,
		Error:    errorMsg,
	})
}

// Local helper struct equivalent to scripts/proxy_scraper.go Proxy config
type LocalProxyConfig struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func parseProxyStringLocal(line string, defaultProtocol string) (*LocalProxyConfig, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return nil, nil
	}

	schema := "http"
	if defaultProtocol != "" {
		schema = defaultProtocol
	}
	if strings.Contains(line, "://") {
		parts := strings.SplitN(line, "://", 2)
		schema = parts[0]
		line = parts[1]
	}

	var host, port, user, pass string

	if strings.Contains(line, "@") {
		parts := strings.SplitN(line, "@", 2)
		authPart := parts[0]
		connPart := parts[1]

		authParts := strings.SplitN(authPart, ":", 2)
		if len(authParts) == 2 {
			user = authParts[0]
			pass = authParts[1]
		} else {
			user = authParts[0]
		}

		connParts := strings.SplitN(connPart, ":", 2)
		if len(connParts) == 2 {
			host = connParts[0]
			port = connParts[1]
		} else {
			return nil, fmt.Errorf("invalid format")
		}
	} else {
		parts := strings.Split(line, ":")
		if len(parts) == 4 {
			host = parts[0]
			port = parts[1]
			user = parts[2]
			pass = parts[3]
		} else if len(parts) == 2 {
			host = parts[0]
			port = parts[1]
		} else if len(parts) == 3 {
			host = parts[0]
			port = parts[1]
			user = parts[2]
		} else {
			return nil, fmt.Errorf("invalid format")
		}
	}

	return &LocalProxyConfig{
		Scheme:   schema,
		Host:     host,
		Port:     port,
		Username: user,
		Password: pass,
	}, nil
}

func testProxyLocal(p *LocalProxyConfig, targetURL string, timeout time.Duration) (int, error) {
	var proxyStr string
	if p.Username != "" && p.Password != "" {
		proxyStr = fmt.Sprintf("%s://%s:%s@%s:%s", p.Scheme, url.PathEscape(p.Username), url.PathEscape(p.Password), p.Host, p.Port)
	} else {
		proxyStr = fmt.Sprintf("%s://%s:%s", p.Scheme, p.Host, p.Port)
	}

	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return 0, err
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Read up to 512 bytes
	var buf [512]byte
	_, _ = resp.Body.Read(buf[:])

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return 0, fmt.Errorf("status HTTP error: %d", resp.StatusCode)
	}

	return int(time.Since(start).Milliseconds()), nil
}

type ProxyToolRequest struct {
	Proxies         string `json:"proxies"`
	ScrapePublic    bool   `json:"scrape_public"`
	TargetURL       string `json:"target_url"`
	DefaultProtocol string `json:"default_protocol"`
}

type ProxyToolResultItem struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Protocol  string `json:"protocol"`
	LatencyMS int    `json:"latency_ms"`
	Formatted string `json:"formatted"`
}

type ProxyToolResponse struct {
	Success bool                  `json:"success"`
	Results []ProxyToolResultItem `json:"results"`
}

// POST /dashboard/proxies/tool/test
func (s *Server) proxyToolTest(w http.ResponseWriter, r *http.Request) {
	var req ProxyToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.(http.Flusher)

	var logMutex sync.Mutex
	writeLog := func(format string, args ...any) {
		logMutex.Lock()
		defer logMutex.Unlock()
		msg := fmt.Sprintf(format, args...)
		_, _ = fmt.Fprint(w, msg+"\n")
		if hasFlusher {
			flusher.Flush()
		}
	}

	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		targetURL = "https://news.google.com/rss"
	} else {
		parsed, err := url.Parse(targetURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeLog("[ERROR] Target URL tidak valid. Harus menggunakan http atau https.")
			return
		}
		// SSRF protection: block localhost and private IP subnets / hostnames
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "172.16.") || strings.HasPrefix(host, "172.17.") || strings.HasPrefix(host, "172.18.") || strings.HasPrefix(host, "172.19.") || strings.HasPrefix(host, "172.2") || strings.HasPrefix(host, "172.3") {
			writeLog("[ERROR] Target URL tidak boleh berupa alamat lokal/privat (SSRF protection).")
			return
		}
	}

	writeLog("[INFO] Memulai proses pencarian proxy...")

	var rawProxies []string
	defaultProto := strings.TrimSpace(req.DefaultProtocol)
	if defaultProto == "" {
		defaultProto = "http"
	}

	// 1. Scrape public if requested
	if req.ScrapePublic {
		var publicURLs []string
		if defaultProto == "socks5" {
			publicURLs = []string{
				"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&timeout=3000&country=all&ssl=all&anonymity=all",
				"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/socks5.txt",
			}
		} else {
			publicURLs = []string{
				"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=3000&country=all&ssl=all&anonymity=all",
				"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
			}
		}

		for _, apiURL := range publicURLs {
			writeLog("[INFO] Mengambil proxy dari: %s...", apiURL)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(apiURL)
			if err != nil {
				writeLog("[WARN] Gagal menghubungi API sumber: %v", err)
				continue
			}

			count := 0
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" && !strings.HasPrefix(line, "#") {
					rawProxies = append(rawProxies, line)
					count++
				}
			}
			resp.Body.Close()
			writeLog("[INFO] Berhasil memuat %d proxy.", count)
		}

		// Limit to top 150 public proxies to avoid overloading or taking too long
		if len(rawProxies) > 150 {
			rawProxies = rawProxies[:150]
			writeLog("[INFO] Membatasi pengecekan proxy publik ke 150 proxy teratas.")
		}
	}

	// 2. Add custom proxies pasted by user
	lines := strings.Split(req.Proxies, "\n")
	customCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			rawProxies = append(rawProxies, line)
			customCount++
		}
	}
	if customCount > 0 {
		writeLog("[INFO] Memuat %d proxy kustom dari input.", customCount)
	}

	if len(rawProxies) == 0 {
		writeLog("[WARN] Tidak ada proxy untuk diuji.")
		return
	}

	// Parse them
	var parsed []*LocalProxyConfig
	for _, raw := range rawProxies {
		p, err := parseProxyStringLocal(raw, defaultProto)
		if err == nil && p != nil {
			parsed = append(parsed, p)
		}
	}

	writeLog("[INFO] Memulai pengujian %d proxy secara paralel ke %s...", len(parsed), targetURL)

	// Test concurrently
	var wg sync.WaitGroup
	sem := make(chan struct{}, 30)
	timeout := 4 * time.Second
	successCount := 0

	for _, p := range parsed {
		wg.Add(1)
		go func(pr *LocalProxyConfig) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			latency, err := testProxyLocal(pr, targetURL, timeout)
			if err == nil {
				writeLog("[OK] %s:%s | Latensi: %dms", pr.Host, pr.Port, latency)
				
				var formatted string
				if pr.Username != "" && pr.Password != "" {
					formatted = fmt.Sprintf("%s://%s:%s:%s:%s", pr.Scheme, pr.Host, pr.Port, pr.Username, pr.Password)
				} else {
					formatted = fmt.Sprintf("%s://%s:%s", pr.Scheme, pr.Host, pr.Port)
				}
				writeLog("[PROXY] %s", formatted)
				
				logMutex.Lock()
				successCount++
				logMutex.Unlock()
			} else {
				writeLog("[FAIL] %s:%s | Error: %v", pr.Host, pr.Port, err)
			}
		}(p)
	}

	wg.Wait()
	writeLog("[SUCCESS] Pengujian selesai! Menemukan %d proxy aktif.", successCount)
}

type ProxyScraperImportRequest struct {
	Proxies  string `json:"proxies"`
	Protocol string `json:"protocol"`
}

// POST /dashboard/proxies/scraper/import
func (s *Server) proxyScraperImport(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req ProxyScraperImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid request body"})
		return
	}

	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = "http"
	}

	lines := strings.Split(req.Proxies, "\n")
	
	existingProxies := s.store.ListProxies()
	existingMap := make(map[string]bool)
	for _, ep := range existingProxies {
		key := fmt.Sprintf("%s:%d", ep.IP, ep.Port)
		existingMap[key] = true
	}

	successCount := 0
	failedCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		pr, err := parseProxyStringLocal(line, protocol)
		if err != nil || pr == nil {
			failedCount++
			continue
		}

		port, err := strconv.Atoi(pr.Port)
		if err != nil || port <= 0 {
			failedCount++
			continue
		}

		key := fmt.Sprintf("%s:%d", pr.Host, port)
		if existingMap[key] {
			// Find existing proxy and trigger recheck/refresh status
			var existingProxy *cms.Proxy
			for _, ep := range existingProxies {
				if ep.IP == pr.Host && ep.Port == port {
					existingProxy = &ep
					break
				}
			}
			if existingProxy != nil {
				_ = s.store.UpdateProxyStatus(existingProxy.ID, "checking", 0)
				go func(pCopy cms.Proxy) {
					latency, err := CheckProxyConnection(pCopy)
					if err != nil {
						_ = s.store.UpdateProxyStatus(pCopy.ID, "dead", 0)
					} else {
						_ = s.store.UpdateProxyStatus(pCopy.ID, "active", latency)
					}
				}(*existingProxy)
				successCount++
			} else {
				failedCount++
			}
			continue
		}

		p, err := s.store.CreateProxy(user, cms.ProxyInput{
			IP:       pr.Host,
			Port:     port,
			Username: pr.Username,
			Password: pr.Password,
			Protocol: pr.Scheme,
		})

		if err != nil {
			failedCount++
			continue
		}

		existingMap[key] = true
		successCount++

		// Trigger check
		go func(pCopy cms.Proxy) {
			latency, err := CheckProxyConnection(pCopy)
			if err != nil {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "dead", 0)
			} else {
				_ = s.store.UpdateProxyStatus(pCopy.ID, "active", latency)
			}
		}(*p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "success_count": successCount, "failed_count": failedCount})
}

func (s *Server) updateProxyScraperSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard/proxies/scraper?error=Form+tidak+valid", http.StatusSeeOther)
		return
	}

	enabled := r.FormValue("proxy_auto_scrape_enabled") == "true" || r.FormValue("proxy_auto_scrape_enabled") == "on"
	threshold := r.FormValue("proxy_auto_scrape_threshold")

	settings := s.store.GetSettings()
	settings["proxy_auto_scrape_enabled"] = strconv.FormatBool(enabled)
	settings["proxy_auto_scrape_threshold"] = threshold

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		http.Redirect(w, r, "/dashboard/proxies/scraper?error="+url.QueryEscape("Gagal memperbarui: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/proxies/scraper?success=Pengaturan+auto-scraper+berhasil+disimpan", http.StatusSeeOther)
}



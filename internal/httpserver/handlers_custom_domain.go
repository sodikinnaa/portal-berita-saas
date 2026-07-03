package httpserver

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"porta-berita/internal/cms"
)

var (
	publicIPCache string
	publicIPMutex sync.Mutex
)

func getPublicIP() string {
	publicIPMutex.Lock()
	defer publicIPMutex.Unlock()
	if publicIPCache != "" {
		return publicIPCache
	}
	
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://ifconfig.me")
	if err == nil {
		defer resp.Body.Close()
		if ip, err := io.ReadAll(resp.Body); err == nil {
			strIP := strings.TrimSpace(string(ip))
			if net.ParseIP(strIP) != nil {
				publicIPCache = strIP
				return publicIPCache
			}
		}
	}

	resp, err = client.Get("https://api.ipify.org")
	if err == nil {
		defer resp.Body.Close()
		if ip, err := io.ReadAll(resp.Body); err == nil {
			strIP := strings.TrimSpace(string(ip))
			if net.ParseIP(strIP) != nil {
				publicIPCache = strIP
				return publicIPCache
			}
		}
	}

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addrs {
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					publicIPCache = ipnet.IP.String()
					return publicIPCache
				}
			}
		}
	}
	publicIPCache = "127.0.0.1"
	return publicIPCache
}

type customDomainSettingViewData struct {
	User      *cms.User
	Settings  map[string]string
	IPAddress string
	Success   string
	Error     string
}

func (s *Server) dashboardCustomDomain(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()

	s.renderTemplate(w, "custom_domain.html", customDomainSettingViewData{
		User:      user,
		Settings:  settings,
		IPAddress: getPublicIP(),
	})
}

func (s *Server) updateCustomDomain(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	existingSettings := s.store.GetSettings()

	rawDomain := r.FormValue("custom_domain")
	domain := strings.TrimSpace(rawDomain)
	// Remove http:// or https:// if user entered it
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimSuffix(domain, "/")
	domain = strings.ToLower(domain)

	if domain == "" {
		s.renderTemplate(w, "custom_domain.html", customDomainSettingViewData{
			User:      user,
			Settings:  existingSettings,
			IPAddress: getPublicIP(),
			Error:     "Nama domain tidak boleh kosong",
		})
		return
	}

	// Basic validation
	if !strings.Contains(domain, ".") || strings.Contains(domain, " ") || strings.Contains(domain, "/") {
		s.renderTemplate(w, "custom_domain.html", customDomainSettingViewData{
			User:      user,
			Settings:  existingSettings,
			IPAddress: getPublicIP(),
			Error:     "Format domain tidak valid",
		})
		return
	}

	// Check DNS resolution
	serverIP := getPublicIP()
	ips, err := net.LookupIP(domain)
	dnsValid := false
	if err == nil {
		for _, ip := range ips {
			if ip.String() == serverIP {
				dnsValid = true
				break
			}
		}
	}

	// Try to write Nginx config if we are on VPS (running as root and /etc/nginx/conf.d exists)
	nginxDir := "/etc/nginx/conf.d"
	nginxConfigured := false
	sslConfigured := false
	var setupErr error

	if fi, statErr := os.Stat(nginxDir); statErr == nil && fi.IsDir() && dnsValid {
		// Get current port
		port := "8080"
		if strings.Contains(s.cfg.Addr, ":") {
			parts := strings.Split(s.cfg.Addr, ":")
			port = parts[len(parts)-1]
		}

		nginxConfPath := filepath.Join(nginxDir, domain+".conf")
		nginxConfContent := fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s www.%s;

    location / {
        proxy_pass http://127.0.0.1:%s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port 80;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 120s;
        proxy_connect_timeout 30s;
        proxy_send_timeout 120s;
    }
}
`, domain, domain, port)

		// Write config
		writeErr := os.WriteFile(nginxConfPath, []byte(nginxConfContent), 0644)
		if writeErr != nil {
			setupErr = fmt.Errorf("gagal menulis konfigurasi Nginx: %v", writeErr)
		} else {
			// Test Nginx
			cmdTest := exec.Command("nginx", "-t")
			if errTest := cmdTest.Run(); errTest != nil {
				// Revert file
				_ = os.Remove(nginxConfPath)
				setupErr = fmt.Errorf("tes konfigurasi Nginx gagal, perubahan dibatalkan: %v", errTest)
			} else {
				// Reload Nginx
				cmdReload := exec.Command("systemctl", "reload", "nginx")
				_ = cmdReload.Run()
				nginxConfigured = true

				// Run Certbot
				cmdCert := exec.Command("certbot", "--nginx", "-d", domain, "--non-interactive", "--agree-tos", "-m", "datadebasa@gmail.com")
				outputBytes, errCert := cmdCert.CombinedOutput()
				if errCert != nil {
					setupErr = fmt.Errorf("SSL (Certbot) gagal diaktifkan: %v. Output: %s", errCert, string(outputBytes))
				} else {
					sslConfigured = true
				}
			}
		}
	}

	// Update settings
	settings := make(map[string]string)
	for k, v := range existingSettings {
		settings[k] = v
	}
	settings["custom_domain"] = domain

	err = s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "custom_domain.html", customDomainSettingViewData{
			User:      user,
			Settings:  settings,
			IPAddress: serverIP,
			Error:     "Gagal menyimpan pengaturan: " + err.Error(),
		})
		return
	}

	// Compose dynamic status message
	var successMsg, errorMsg string
	if !dnsValid {
		successMsg = "Domain berhasil disimpan! Peringatan: DNS A Record domain Anda belum mengarah ke IP " + serverIP + ". Pastikan untuk memperbarui DNS Anda agar website dan SSL dapat diaktifkan otomatis."
	} else if setupErr != nil {
		if nginxConfigured && !sslConfigured {
			successMsg = "Domain berhasil disimpan dan dikoneksikan ke Nginx!"
			errorMsg = "Namun, SSL (Certbot) gagal diaktifkan otomatis: " + setupErr.Error()
		} else {
			errorMsg = "Gagal memproses konfigurasi server otomatis: " + setupErr.Error()
		}
	} else if sslConfigured {
		successMsg = "Sukses: Domain berhasil dihubungkan, Nginx dikonfigurasi, dan SSL HTTPS Let's Encrypt aktif otomatis!"
	} else {
		// Non-VPS environment where /etc/nginx/conf.d doesn't exist
		successMsg = "Domain berhasil disimpan di pengaturan!"
	}

	s.renderTemplate(w, "custom_domain.html", customDomainSettingViewData{
		User:      user,
		Settings:  settings,
		IPAddress: serverIP,
		Success:   successMsg,
		Error:     errorMsg,
	})
}

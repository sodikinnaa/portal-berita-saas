package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Proxy merepresentasikan konfigurasi sebuah proxy
type Proxy struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// FormatString mengembalikan format standard host:port atau host:port:user:pass atau user:pass@host:port
func (p *Proxy) FormatString(style string) string {
	switch style {
	case "auth-at":
		if p.Username != "" && p.Password != "" {
			return fmt.Sprintf("%s://%s:%s@%s:%s", p.Scheme, p.Username, p.Password, p.Host, p.Port)
		}
		return fmt.Sprintf("%s://%s:%s", p.Scheme, p.Host, p.Port)
	case "colon":
		if p.Username != "" && p.Password != "" {
			return fmt.Sprintf("%s:%s:%s:%s", p.Host, p.Port, p.Username, p.Password)
		}
		return fmt.Sprintf("%s:%s", p.Host, p.Port)
	default:
		if p.Username != "" && p.Password != "" {
			return fmt.Sprintf("%s:%s:%s:%s", p.Host, p.Port, p.Username, p.Password)
		}
		return fmt.Sprintf("%s:%s", p.Host, p.Port)
	}
}

// URLString mengembalikan url.URL yang bisa dipakai oleh http.Transport
func (p *Proxy) URLString() string {
	if p.Username != "" && p.Password != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%s", p.Scheme, url.PathEscape(p.Username), url.PathEscape(p.Password), p.Host, p.Port)
	}
	return fmt.Sprintf("%s://%s:%s", p.Scheme, p.Host, p.Port)
}

func parseProxyString(line string) (*Proxy, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return nil, nil
	}

	schema := "http"
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
			return nil, fmt.Errorf("format host:port tidak valid setelah @: %s", connPart)
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
			return nil, fmt.Errorf("format tidak dikenali (butuh host:port atau host:port:user:pass): %s", line)
		}
	}

	return &Proxy{
		Scheme:   schema,
		Host:     host,
		Port:     port,
		Username: user,
		Password: pass,
	}, nil
}

func fetchFromURL(urlStr string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status HTTP tidak OK: %d", resp.StatusCode)
	}

	var proxies []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			proxies = append(proxies, line)
		}
	}
	return proxies, scanner.Err()
}

func testProxy(ctx context.Context, p *Proxy, targetURL string, timeout time.Duration) (time.Duration, error) {
	proxyURLStr := p.URLString()
	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return 0, fmt.Errorf("gagal parse proxy URL: %w", err)
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return 0, err
	}
	// Tambahkan header user-agent standar
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Baca sedikit body untuk memastikan transfer data berhasil
	_, _ = io.CopyN(io.Discard, resp.Body, 1024)

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return 0, fmt.Errorf("status HTTP error: %d", resp.StatusCode)
	}

	return time.Since(start), nil
}

func main() {
	targetFlag := flag.String("target", "https://member.wakdondin.my.id/", "URL target untuk menguji proxy")
	inputFileFlag := flag.String("input", "proxies_input.txt", "File teks lokal berisi daftar proxy kustom Anda (misal dari Webshare)")
	workersFlag := flag.Int("workers", 30, "Jumlah goroutine untuk pengecekan paralel")
	timeoutFlag := flag.Duration("timeout", 5*time.Second, "Batas waktu koneksi tiap proxy")
	outputFileFlag := flag.String("output", "valid_proxies.txt", "File output hasil proxy yang aktif")
	scrapePublicFlag := flag.Bool("scrape-public", true, "Scrape proxy publik gratis dari ProxyScrape & TheSpeedX")

	flag.Parse()

	fmt.Println("==================================================")
	fmt.Println("         PROXY SCRAPER & CHECKER UTILITY         ")
	fmt.Println("==================================================")
	fmt.Printf("Target Uji       : %s\n", *targetFlag)
	fmt.Printf("File Input       : %s\n", *inputFileFlag)
	fmt.Printf("Jumlah Pekerja   : %d\n", *workersFlag)
	fmt.Printf("Batas Waktu      : %v\n", *timeoutFlag)
	fmt.Printf("File Output      : %s\n", *outputFileFlag)
	fmt.Printf("Scrape Publik    : %t\n", *scrapePublicFlag)
	fmt.Println("==================================================")

	var rawProxies []string

	// 1. Membaca file input lokal jika ada (misal list Webshare milik user)
	if _, err := os.Stat(*inputFileFlag); err == nil {
		fmt.Printf("[+] Membaca proxy dari file input: %s...\n", *inputFileFlag)
		file, err := os.Open(*inputFileFlag)
		if err != nil {
			fmt.Printf("[!] Gagal membuka file input: %v\n", err)
		} else {
			scanner := bufio.NewScanner(file)
			count := 0
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					rawProxies = append(rawProxies, line)
					count++
				}
			}
			file.Close()
			fmt.Printf("[+] Berhasil memuat %d proxy dari file input.\n", count)
		}
	} else {
		// Jika file input tidak ada, buatkan template kosong agar user tahu formatnya
		fmt.Printf("[-] File '%s' tidak ditemukan. Membuat template file baru...\n", *inputFileFlag)
		placeholder := "# Masukkan daftar proxy kustom Anda di sini.\n" +
			"# Format yang didukung:\n" +
			"# - IP:Port\n" +
			"# - IP:Port:Username:Password (Format Webshare)\n" +
			"# - Username:Password@IP:Port\n" +
			"# Contoh:\n" +
			"# 182.25.10.11:8080\n" +
			"# 145.22.11.90:80:user123:pass456\n" +
			"# user123:pass456@145.22.11.90:80\n"
		_ = os.WriteFile(*inputFileFlag, []byte(placeholder), 0644)
		fmt.Printf("[+] Template '%s' telah dibuat. Anda bisa memasukkan proxy Webshare Anda di sana nanti.\n", *inputFileFlag)
	}

	// 2. Scrape dari public list jika diaktifkan
	if *scrapePublicFlag {
		publicURLs := []string{
			"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=5000&country=all&ssl=all&anonymity=all",
			"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
		}

		for _, apiURL := range publicURLs {
			fmt.Printf("[+] Mengambil proxy publik dari: %s...\n", apiURL)
			proxies, err := fetchFromURL(apiURL)
			if err != nil {
				fmt.Printf("[!] Gagal mengambil proxy publik: %v\n", err)
				continue
			}
			rawProxies = append(rawProxies, proxies...)
			fmt.Printf("[+] Mendapatkan %d proxy dari API publik.\n", len(proxies))
		}
	}

	if len(rawProxies) == 0 {
		fmt.Println("[!] Tidak ada proxy yang ditemukan untuk dicek. Silakan isi file input atau aktifkan scrape-public.")
		return
	}

	// Parse semua proxy mentah
	var parsedProxies []*Proxy
	for _, raw := range rawProxies {
		p, err := parseProxyString(raw)
		if err != nil {
			// Lewati baris yang salah format atau komentar
			continue
		}
		if p != nil {
			parsedProxies = append(parsedProxies, p)
		}
	}

	totalProxies := len(parsedProxies)
	fmt.Printf("[+] Memulai pengecekan %d proxy dengan %d pekerja paralel...\n", totalProxies, *workersFlag)

	// Persiapan channel dan synchronization
	proxyChan := make(chan *Proxy, totalProxies)
	for _, p := range parsedProxies {
		proxyChan <- p
	}
	close(proxyChan)

	var wg sync.WaitGroup
	var activeMutex sync.Mutex
	var activeProxies []*Proxy
	var activeProxyStrings []string

	// Start workers
	for i := 0; i < *workersFlag; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for p := range proxyChan {
				ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
				latency, err := testProxy(ctx, p, *targetFlag, *timeoutFlag)
				cancel()

				if err == nil {
					activeMutex.Lock()
					activeProxies = append(activeProxies, p)
					// Format standard untuk diexport/dipakai manual
					formatted := p.FormatString("colon")
					activeProxyStrings = append(activeProxyStrings, formatted)

					fmt.Printf("[OK] %s:%s | Latensi: %v | Auth: %t\n", p.Host, p.Port, latency.Round(time.Millisecond), p.Username != "")
					activeMutex.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	// 3. Tulis hasil akhir
	fmt.Println("==================================================")
	fmt.Printf("[+] Pengecekan selesai! Menulis hasil ke %s...\n", *outputFileFlag)

	if len(activeProxies) == 0 {
		fmt.Println("[!] Tidak ada proxy aktif yang berhasil terhubung ke target.")
		return
	}

	// Tulis file teks kustom (format colon)
	outputData := strings.Join(activeProxyStrings, "\n") + "\n"
	err := os.WriteFile(*outputFileFlag, []byte(outputData), 0644)
	if err != nil {
		fmt.Printf("[!] Gagal menulis file output teks: %v\n", err)
	}

	// Tulis juga versi JSON yang lengkap terstruktur agar mudah di-import sistem lain
	jsonFile := strings.Replace(*outputFileFlag, ".txt", ".json", 1)
	if !strings.HasSuffix(jsonFile, ".json") {
		jsonFile = *outputFileFlag + ".json"
	}

	jsonData, err := json.MarshalIndent(activeProxies, "", "  ")
	if err == nil {
		_ = os.WriteFile(jsonFile, jsonData, 0644)
		fmt.Printf("[+] Berhasil menulis file output JSON lengkap di: %s\n", jsonFile)
	}

	fmt.Printf("[+] Sukses! %d dari %d proxy aktif dan dapat digunakan.\n", len(activeProxies), totalProxies)
	fmt.Println("==================================================")
}

package httpserver

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) initializeDefaultCustomHomepage() {
	baseDir := filepath.Join(s.cfg.UploadDir, "custom_homepage")
	indexPath := filepath.Join(baseDir, "index.html")

	shouldOverwrite := false
	if htmlContent, err := os.ReadFile(indexPath); err == nil {
		content := string(htmlContent)
		// Check if it is an old default template that needs updating
		hasOldWhatsApp := strings.Contains(content, "https://wa.me/6281234567890")
		hasPTBranding := strings.Contains(content, "PT Siapdigital")
		hasOldDemoLink := strings.Contains(content, "Coba Demo Portal 📰")
		hasOldRegisterLink := strings.Contains(content, "https://meowcing.my.id/register") && !strings.Contains(content, "Didukung oleh Platform & Keamanan Terbaik")
		hasOldPlaceholder := strings.Contains(content, "Selamat Datang di Portal Berita Kustom Saya") || strings.Contains(content, "Halaman Utama Kustom")

		if hasOldWhatsApp || hasPTBranding || hasOldDemoLink || hasOldRegisterLink || hasOldPlaceholder {
			shouldOverwrite = true
		}
	} else {
		// File does not exist, write it
		shouldOverwrite = true
	}

	if !shouldOverwrite {
		return
	}

	s.log.Info("attempting to fetch default custom homepage from SaaS backend...")
	
	// Fetch SaaS backend URL from settings
	settings := s.store.GetSettings()
	endpointURL := strings.TrimSpace(settings["ai_endpoint_url"])
	if endpointURL == "" {
		endpointURL = "https://meowcing.my.id" // default production fallback
	}
	// Ensure no trailing slash or duplicate api routes
	endpointURL = strings.TrimSuffix(endpointURL, "/")
	endpointURL = strings.TrimSuffix(endpointURL, "/api/v1")
	endpointURL = strings.TrimSuffix(endpointURL, "/api")

	// Download ZIP from SaaS backend
	url := endpointURL + "/api/public/default-landing-page"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		
		// Read all zip data into memory
		zipData, err := io.ReadAll(resp.Body)
		if err == nil {
			// Extract zip data to baseDir
			err = s.extractHomepageZip(zipData, baseDir)
			if err == nil {
				s.log.Info("successfully updated default custom homepage from SaaS backend!")
				return
			}
			s.log.Error("failed to extract downloaded default custom homepage zip", "error", err)
		} else {
			s.log.Error("failed to read downloaded default custom homepage response", "error", err)
		}
	} else {
		if err != nil {
			s.log.Error("failed to connect to SaaS backend to fetch default landing page", "url", url, "error", err)
		} else {
			s.log.Error("SaaS backend returned non-OK status for default landing page", "url", url, "status", resp.StatusCode)
		}
	}

	// Local fallback: if SaaS fetch failed, write built-in constants
	s.log.Info("falling back to built-in local default custom homepage templates...")

	// Create directories
	_ = os.MkdirAll(baseDir, 0755)
	_ = os.MkdirAll(filepath.Join(baseDir, "subpage"), 0755)
	_ = os.MkdirAll(filepath.Join(baseDir, "about-us"), 0755)

	// Write default templates
	_ = os.WriteFile(filepath.Join(baseDir, "index.html"), []byte(defaultCustomHomepageHTML), 0644)
	_ = os.WriteFile(filepath.Join(baseDir, "style.css"), []byte(defaultCustomHomepageCSS), 0644)
	_ = os.WriteFile(filepath.Join(baseDir, "script.js"), []byte(defaultCustomHomepageJS), 0644)
	_ = os.WriteFile(filepath.Join(baseDir, "subpage", "index.html"), []byte(defaultCustomHomepageSubpageHTML), 0644)
	_ = os.WriteFile(filepath.Join(baseDir, "about-us", "index.html"), []byte(defaultCustomHomepageAboutHTML), 0644)
}

func (s *Server) extractHomepageZip(zipData []byte, destDir string) error {
	// Create reader
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	// Clean destination directory
	_ = os.RemoveAll(destDir)
	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		return err
	}

	// Extract files
	for _, f := range reader.File {
		// Prevent Zip Slip vulnerability
		filePath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filePath, filepath.Clean(destDir)+string(os.PathSeparator)) && filePath != destDir {
			continue // skip unsafe paths
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return err
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		srcFile, err := f.Open()
		if err != nil {
			dstFile.Close()
			return err
		}

		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

const defaultCustomHomepageHTML = `<!DOCTYPE html>
<html lang="id">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Siap Digital - Sewa Sistem Portal Berita All-in-One Premium</title>
    <!-- Google Fonts for Elegant & Tech Typography -->
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700;800&family=Plus+Jakarta+Sans:wght@400;600;700;800&display=swap" rel="stylesheet">
    <link rel="stylesheet" href="style.css">
    <style>
        /* Extra custom styles for marketing page */
        .pricing-section {
            padding: 80px 0;
            background: rgba(255, 255, 255, 0.02);
        }
        .pricing-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
            gap: 40px;
            margin-top: 50px;
            justify-content: center;
        }
        .pricing-card {
            background-color: var(--card-bg);
            border: 1.5px solid var(--border);
            border-radius: 20px;
            padding: 40px;
            text-align: center;
            position: relative;
            transition: var(--transition);
            display: flex;
            flex-direction: column;
            box-shadow: 0 10px 30px rgba(0,0,0,0.02);
        }
        .pricing-card.popular {
            border-color: var(--primary);
            transform: scale(1.05);
        }
        .pricing-card.popular::before {
            content: "PILIHAN TERBAIK";
            position: absolute;
            top: -15px;
            left: 50%;
            transform: translateX(-50%);
            background: linear-gradient(135deg, var(--primary), #ffa000);
            color: #000000;
            font-weight: 800;
            font-size: 11px;
            padding: 6px 18px;
            border-radius: 30px;
            letter-spacing: 1px;
        }
        .pricing-price {
            font-size: 44px;
            font-weight: 800;
            color: var(--text-color);
            margin: 20px 0;
        }
        .pricing-price span {
            font-size: 16px;
            color: var(--text-muted);
            font-weight: normal;
        }
        .pricing-features {
            list-style: none;
            padding: 0;
            margin: 30px 0;
            text-align: left;
            flex-grow: 1;
        }
        .pricing-features li {
            padding: 10px 0;
            color: var(--text-muted);
            font-size: 14px;
            display: flex;
            align-items: center;
            gap: 10px;
        }
        .pricing-features li::before {
            content: "✓";
            color: var(--primary);
            font-weight: bold;
            font-size: 16px;
        }
        .cta-btn {
            background: linear-gradient(135deg, var(--primary), #ffa000);
            color: #000000;
            border: none;
            padding: 15px 30px;
            font-weight: 700;
            border-radius: 12px;
            font-size: 15px;
            cursor: pointer;
            transition: var(--transition);
            text-decoration: none;
            display: inline-block;
        }
        .cta-btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 20px rgba(255, 179, 0, 0.3);
        }
        .cta-btn.secondary {
            background: transparent;
            border: 2px solid var(--border);
            color: var(--text-color);
        }
        .cta-btn.secondary:hover {
            background: rgba(255, 255, 255, 0.05);
            box-shadow: none;
        }
        .feature-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 30px;
            margin-top: 40px;
        }
        .feature-card {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 16px;
            padding: 30px;
            transition: var(--transition);
        }
        .feature-card:hover {
            border-color: var(--primary);
            transform: translateY(-3px);
        }
        .feature-icon {
            font-size: 32px;
            margin-bottom: 15px;
            display: inline-block;
        }
        .badge-new {
            background: rgba(255, 179, 0, 0.1);
            color: var(--primary);
            padding: 4px 10px;
            font-size: 10px;
            font-weight: 800;
            border-radius: 30px;
            margin-left: 8px;
            vertical-align: middle;
        }
    </style>
</head>
<body>

    <!-- Top Announcement Bar -->
    <div class="announcement-bar">
        <span>🔥 Solusi Publikasi Berita Instan: Web Dinamis + Aplikasi Android Membership v1.0.0</span>
    </div>

    <!-- Header Section -->
    <header class="site-header">
        <div class="container">
            <div class="logo-area" style="display: flex; align-items: center; gap: 12px;">
                {{if index .Settings "site_favicon"}}
                <img src="{{index .Settings "site_favicon"}}" alt="Logo" style="height: 40px; width: 40px; border-radius: 50%; object-fit: cover;">
                {{end}}
                <div>
                    <span class="logo-text" style="font-size: 22px; font-weight: 900; color: var(--text-color); letter-spacing: -0.5px; font-family: 'Plus Jakarta Sans', sans-serif;">SIAP<span> DIGITAL</span></span>
                    <p class="logo-tagline" style="margin-top: 2px; font-size: 9px; letter-spacing: 1px; font-weight: bold; color: var(--primary);">JASA SEWA SISTEM PORTAL BERITA ALL-IN-ONE</p>
                </div>
            </div>
            <nav class="main-nav">
                <a href="#features">Fitur Utama</a>
                <a href="#pricing">Paket Harga</a>
                <a href="#about">Tentang Kami</a>
                <a href="/portal" class="portal-link" style="background: var(--primary); color: #000; font-weight: bold;">Coba Demo Portal 📰</a>
                <button id="theme-toggle-btn" class="theme-btn" title="Ganti Tema">🌙</button>
            </nav>
        </div>
    </header>

    <main class="container">
        <!-- Hero Section -->
        <section class="hero-section" style="margin: 50px 0 80px 0;">
            <div class="hero-image" style="background-image: linear-gradient(to bottom, rgba(15, 23, 42, 0.4), rgba(15, 23, 42, 0.95)), url('https://images.unsplash.com/photo-1504711434969-e33886168f5c?auto=format&fit=crop&w=1200&q=80'); min-height: 520px; display: flex; align-items: center; justify-content: center; text-align: center;">
                <div class="hero-content" style="max-width: 900px; padding: 40px 20px;">
                    <span class="category-badge" style="background: linear-gradient(135deg, var(--primary), #ffa000); color: #000; font-weight: 800;">SIAP DIGITAL INDONESIA</span>
                    <h1 class="hero-title" style="font-size: 42px; font-weight: 900; color: #fff; line-height: 1.25; font-family: 'Plus Jakarta Sans', sans-serif; margin-bottom: 20px;">
                        Miliki Media Berita Profesional Anda Sendiri Dalam Sekejap
                    </h1>
                    <p class="hero-excerpt" style="font-size: 17px; color: rgba(255,255,255,0.8); max-width: 780px; margin: 0 auto 35px auto; line-height: 1.7;">
                        Sewa platform portal berita all-in-one super lengkap. Dilengkapi dengan auto-posting Facebook, sistem paywall artikel premium, aplikasi Android eksklusif untuk membership, serta fitur auto-rewrite kecerdasan buatan (AI) berbasis RSS Google News.
                    </p>
                    <div style="display: flex; gap: 15px; justify-content: center; flex-wrap: wrap;">
                        <a href="#pricing" class="cta-btn">Lihat Paket Sewa</a>
                        <a href="/portal" class="cta-btn secondary" style="color: #fff; border-color: rgba(255,255,255,0.3);">Coba Demo Website</a>
                    </div>
                </div>
            </div>
        </section>

        <!-- Features Section -->
        <section id="features" style="padding: 50px 0;">
            <div style="text-align: center; max-width: 700px; margin: 0 auto 60px auto;">
                <h2 class="section-title" style="display: inline-block; font-family: 'Plus Jakarta Sans', sans-serif; font-size: 30px; font-weight: 800;">Fitur Unggulan Sistem Kami</h2>
                <p style="color: var(--text-muted); margin-top: 15px;">Didesain khusus untuk membantu Anda mengelola media berita berkinerja tinggi, kaya konten otomatis, serta ramah monetisasi.</p>
            </div>
            
            <div class="feature-grid">
                <div class="feature-card">
                    <span class="feature-icon">📢</span>
                    <h3 style="font-size: 18px; font-weight: 700; margin-bottom: 12px; font-family: 'Plus Jakarta Sans', sans-serif;">Auto-Posting Facebook</h3>
                    <p style="font-size: 13.5px; color: var(--text-muted); line-height: 1.6;">Posting setiap berita secara instan ke Halaman/Fanspage Facebook Anda secara otomatis saat diterbitkan. <br><em style="font-size: 11px; color: var(--primary);">*Syarat: Memiliki akun bisnis Facebook terverifikasi/disetujui.</em></p>
                </div>
                
                <div class="feature-card">
                    <span class="feature-icon">🤖</span>
                    <h3 style="font-size: 18px; font-weight: 700; margin-bottom: 12px; font-family: 'Plus Jakarta Sans', sans-serif;">Auto-Rewrite & RSS Scraper</h3>
                    <p style="font-size: 13.5px; color: var(--text-muted); line-height: 1.6;">Scrape konten berita otomatis secara berkala dari Google News RSS. AI secara cerdas menulis ulang judul dan isi artikel agar unik tanpa kehilangan arti aslinya, lengkap dengan atribusi sumber asli serta **Proxy Rotator** built-in agar terhindar dari blokir IP.</p>
                </div>
                
                <div class="feature-card">
                    <span class="feature-icon">🔒</span>
                    <h3 style="font-size: 18px; font-weight: 700; margin-bottom: 12px; font-family: 'Plus Jakarta Sans', sans-serif;">Sistem Konten Premium</h3>
                    <p style="font-size: 13.5px; color: var(--text-muted); line-height: 1.6;">Kunci artikel eksklusif atau opini mendalam Anda di balik fitur paywall. Hanya pelanggan berbayar (premium) yang dapat membaca berita khusus ini.</p>
                </div>
                
                <div class="feature-card">
                    <span class="feature-icon">📱</span>
                    <h3 style="font-size: 18px; font-weight: 700; margin-bottom: 12px; font-family: 'Plus Jakarta Sans', sans-serif;">Aplikasi Android Membership <span class="badge-new">NEW</span></h3>
                    <p style="font-size: 13.5px; color: var(--text-muted); line-height: 1.6;">Aplikasi native Android berkinerja tinggi bagi pelanggan Anda untuk membaca konten premium. Sistem manajemen member dan konfirmasi pembayaran dilakukan secara manual melalui dashboard admin demi keamanan transaksi.</p>
                </div>
            </div>
        </section>

        <!-- Pricing Section -->
        <section id="pricing" class="pricing-section">
            <div style="text-align: center; max-width: 700px; margin: 0 auto;">
                <h2 class="section-title" style="display: inline-block; font-family: 'Plus Jakarta Sans', sans-serif; font-size: 30px; font-weight: 800;">Pilihan Paket Sewa Sistem</h2>
                <p style="color: var(--text-muted); margin-top: 15px;">Investasi terbaik untuk membesarkan media berita digital Anda secara autopilot dan profesional.</p>
            </div>
            
            <div id="pricing-plans-grid" class="pricing-grid">
                <!-- Web Only -->
                <div class="pricing-card">
                    <h3 style="font-size: 20px; font-weight: 700; font-family: 'Plus Jakarta Sans', sans-serif;">WEB ONLY PACKAGE</h3>
                    <p style="font-size: 12px; color: var(--text-muted); margin-top: 5px;">Portal Berita Dinamis Berkinerja Tinggi</p>
                    <div class="pricing-price">Rp2.000.000<span>/bulan</span></div>
                    <ul class="pricing-features">
                        <li>Domain Kustom Sendiri (.com / .id / dsb.)</li>
                        <li>Sistem CMS Admin & Penulis Super Lengkap</li>
                        <li>Auto Scraper RSS & Auto AI Rewrite</li>
                        <li>Built-in Proxy Rotator (Anti Blokir)</li>
                        <li>Facebook Auto-Posting System</li>
                        <li>Sistem Konten Premium (Paywall)</li>
                        <li>Server VPS & Cloud Hosting Premium</li>
                        <li>Sertifikat Keamanan SSL (HTTPS) Gratis</li>
                        <li><strong>Free Biaya Update Fitur Baru</strong></li>
                    </ul>
                    <a href="https://meowcing.my.id/register?plan=2" class="cta-btn secondary">Sewa Paket Web Only</a>
                </div>
                
                <!-- Web & APK -->
                <div class="pricing-card popular">
                    <h3 style="font-size: 20px; font-weight: 700; font-family: 'Plus Jakarta Sans', sans-serif;">WEB & APK PACKAGE</h3>
                    <p style="font-size: 12px; color: var(--text-muted); margin-top: 5px;">Ekosistem Portal Berita + Aplikasi Mobile Lengkap</p>
                    <div class="pricing-price">Rp14.000.000<span>/bulan</span></div>
                    <ul class="pricing-features">
                        <li><strong>Semua Fitur di Paket Web Only</strong></li>
                        <li><strong>Aplikasi Android Native (.APK / .AAB)</strong></li>
                        <li><strong>Fitur Membership Akun & Login di Aplikasi</strong></li>
                        <li><strong>Akses Konten Premium via Aplikasi Android</strong></li>
                        <li>Sistem Notifikasi Push (Kirim Berita ke Layar HP) <span class="badge-new" style="background: rgba(255, 179, 0, 0.15); color: var(--primary); font-size: 9px; padding: 2px 6px;">ON-GOING</span></li>
                        <li>Setup & Publikasi Aplikasi ke Google Play Store</li>
                        <li>Pembaruan & Pemeliharaan Aplikasi Berkala</li>
                        <li>Setup Pembayaran & Verifikasi Member Manual Aman</li>
                        <li><strong>Free Biaya Update Fitur Baru</strong></li>
                    </ul>
                    <a href="https://meowcing.my.id/register?plan=3" class="cta-btn">Sewa Paket Web & APK</a>
                </div>
            </div>
        </section>

        <!-- Partner Logo Ticker (SaaS Inspired) -->
        <div class="logo-ticker">
            <div class="container">
                <div class="logo-ticker-title">Didukung oleh Platform & Keamanan Terbaik</div>
                <div class="logo-ticker-inner">
                    <div class="ticker-logo">📰 Google News API</div>
                    <div class="ticker-logo">📘 Facebook Graph</div>
                    <div class="ticker-logo">🤖 OpenAI Gemini</div>
                    <div class="ticker-logo">📱 Android Native</div>
                    <div class="ticker-logo">🛡️ Cloudflare</div>
                </div>
            </div>
        </div>

        <!-- Testimonials Section (SaaS Inspired) -->
        <section class="testimonials-section">
            <div style="text-align: center; max-width: 700px; margin: 0 auto 40px auto;">
                <h2 class="section-title" style="display: inline-block; font-family: 'Plus Jakarta Sans', sans-serif; font-size: 30px; font-weight: 800;">Dipercaya oleh Pemilik Media</h2>
                <p style="color: var(--text-muted); margin-top: 15px;">Apa kata mereka yang telah mendigitalisasi media beritanya bersama Siap Digital.</p>
            </div>
            <div class="testimonials-grid">
                <div class="testimonial-card">
                    <p class="testimonial-quote">"Sewa sistem di Siap Digital memotong biaya server dan development hingga 80%. Semuanya berjalan sangat autopilot dan lancar!"</p>
                    <div class="testimonial-profile">
                        <div class="testimonial-avatar">BS</div>
                        <div class="testimonial-info">
                            <h4>Budi Santoso</h4>
                            <p>Founder MediaLokal.id</p>
                        </div>
                    </div>
                </div>
                <div class="testimonial-card">
                    <p class="testimonial-quote">"Fitur AI auto-rewrite Google News sangat mempermudah pengisian berita dasar harian kami secara instan dan unik tanpa terkena duplikasi."</p>
                    <div class="testimonial-profile">
                        <div class="testimonial-avatar">RW</div>
                        <div class="testimonial-info">
                            <h4>Rina Wijaya</h4>
                            <p>Editor InfoKota.com</p>
                        </div>
                    </div>
                </div>
                <div class="testimonial-card">
                    <p class="testimonial-quote">"Aplikasi Android membership mempermudah pembaca setia kami untuk berlangganan dan mengakses berita premium secara eksklusif."</p>
                    <div class="testimonial-profile">
                        <div class="testimonial-avatar">AF</div>
                        <div class="testimonial-info">
                            <h4>Ahmad Fauzi</h4>
                            <p>CEO KabarBanten.net</p>
                        </div>
                    </div>
                </div>
            </div>
        </section>

        <!-- FAQ Section (SaaS Inspired) -->
        <section class="faq-section">
            <div style="text-align: center; max-width: 700px; margin: 0 auto;">
                <h2 class="section-title" style="display: inline-block; font-family: 'Plus Jakarta Sans', sans-serif; font-size: 30px; font-weight: 800;">Pertanyaan yang Sering Diajukan</h2>
                <p style="color: var(--text-muted); margin-top: 15px;">Segala hal yang perlu Anda ketahui tentang sistem portal berita kami.</p>
            </div>
            <div class="faq-list">
                <div class="faq-item">
                    <div class="faq-question">Apakah saya perlu membeli VPS atau server sendiri? <span class="faq-toggle-icon">▼</span></div>
                    <div class="faq-answer">
                        <div class="faq-answer-inner">Tidak perlu. Semua paket sewa kami sudah mencakup server VPS performa tinggi, pemeliharaan berkala, serta sertifikat keamanan SSL gratis. Anda tinggal fokus memproduksi berita.</div>
                    </div>
                </div>
                <div class="faq-item">
                    <div class="faq-question">Bagaimana cara kerja auto-posting Facebook? <span class="faq-toggle-icon">▼</span></div>
                    <div class="faq-answer">
                        <div class="faq-answer-inner">Setiap kali Anda atau AI menerbitkan artikel baru di web portal, sistem kami akan langsung membagikannya secara otomatis ke Fanspage Facebook Anda melalui integrasi Facebook API secara real-time.</div>
                    </div>
                </div>
                <div class="faq-item">
                    <div class="faq-question">Bagaimana status fitur notifikasi push? <span class="faq-toggle-icon">▼</span></div>
                    <div class="faq-answer">
                        <div class="faq-answer-inner">Fitur notifikasi push ke layar HP pembaca saat ini sedang dalam proses pengembangan aktif (ON-GOING) dan akan dirilis dalam waktu dekat. Semua penyewa aktif akan mendapatkan update fitur baru ini secara GRATIS tanpa biaya tambahan.</div>
                    </div>
                </div>
                <div class="faq-item">
                    <div class="faq-question">Apakah pembayaran member di aplikasi Android aman? <span class="faq-toggle-icon">▼</span></div>
                    <div class="faq-answer">
                        <div class="faq-answer-inner">Sangat aman. Saat ini sistem kami menggunakan konfirmasi pembayaran manual melalui dashboard admin. Pembaca mengirim bukti pembayaran, admin melakukan verifikasi sekali klik, dan akun member premium langsung aktif.</div>
                    </div>
                </div>
            </div>
        </section>

        <!-- Static About Section -->
        <section id="about" class="about-section" style="margin-top: 60px;">
            <div class="about-content">
                <h2 style="font-family: 'Plus Jakarta Sans', sans-serif; font-weight: 800; font-size: 26px;">Tentang Siap Digital</h2>
                <p style="line-height: 1.7; font-size: 14.5px;">Siap Digital adalah penyedia layanan teknologi yang berdedikasi membangun infrastruktur media informasi cerdas. Kami menawarkan solusi sewa sistem berita modular yang tangguh, aman, dan canggih, menghilangkan kerumitan manajemen teknis, server, dan koding agar Anda dapat berfokus 100% pada pembuatan konten berkualitas dan pertumbuhan bisnis.</p>
                <div class="disclaimer" style="line-height: 1.6; font-size: 12.5px;">
                    <strong>ℹ️ Catatan Sistem:</strong> Sistem manajemen member dan aktivasi langganan premium pada aplikasi Android saat ini dilakukan secara manual oleh administrator untuk menjamin validitas dan keamanan transaksi keuangan yang optimal sebelum kami meluncurkan gerbang pembayaran otomatis di versi berikutnya.
                </div>
            </div>
        </section>
    </main>

    <!-- Footer Section -->
    <footer class="site-footer">
        <div class="container">
            <p>&copy; <span id="current-year">2026</span> Siap Digital. Hak Cipta Dilindungi.</p>
            <p style="margin-top: 8px; font-size: 12px; color: var(--text-muted);">Partner Solusi Digitalisasi Media Informasi Anda.</p>
        </div>
    </footer>

    <script src="script.js"></script>
</body>
</html>`

const defaultCustomHomepageCSS = `/* Global Variables & Reset */
:root {
    --primary: #ffb300;
    --primary-hover: #ffa000;
    --background: #f8fafc;
    --text-color: #0f172a;
    --text-muted: #475569;
    --header-bg: #ffffff;
    --card-bg: #ffffff;
    --border: #e2e8f0;
    --announcement-bg: #0f172a;
    --announcement-text: #ffffff;
    --transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

/* Dark Mode Variables */
[data-theme="dark"] {
    --background: #090d16;
    --text-color: #f8fafc;
    --text-muted: #94a3b8;
    --header-bg: #0f172a;
    --card-bg: #0f172a;
    --border: #1e293b;
    --announcement-bg: #090d16;
    --announcement-text: #f8fafc;
}

* {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
}

body {
    font-family: 'Outfit', sans-serif;
    background-color: var(--background);
    color: var(--text-color);
    line-height: 1.5;
    transition: var(--transition);
}

.container {
    width: 90%;
    max-width: 1200px;
    margin: 0 auto;
}

/* Announcement Bar */
.announcement-bar {
    background-color: var(--announcement-bg);
    color: var(--announcement-text);
    text-align: center;
    padding: 8px 0;
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.5px;
    transition: var(--transition);
}

/* Header & Navigation */
.site-header {
    background-color: var(--header-bg);
    border-bottom: 1px solid var(--border);
    position: sticky;
    top: 0;
    z-index: 100;
    padding: 15px 0;
    box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
    transition: var(--transition);
}

.site-header .container {
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.logo-text {
    font-family: 'Playfair Display', serif;
    font-size: 26px;
    font-weight: 900;
    color: var(--text-color);
    letter-spacing: -0.5px;
}

.logo-text span {
    color: var(--primary);
}

.logo-tagline {
    font-size: 10px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 1.5px;
    margin-top: -2px;
}

.main-nav {
    display: flex;
    gap: 20px;
    align-items: center;
}

.main-nav a {
    text-decoration: none;
    color: var(--text-color);
    font-weight: 600;
    font-size: 14px;
    transition: var(--transition);
    padding: 6px 12px;
    border-radius: 6px;
}

.main-nav a:hover {
    color: var(--primary);
    background-color: rgba(230, 51, 41, 0.05);
}

.theme-btn {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 16px;
    padding: 8px 12px;
    border-radius: 50%;
    transition: var(--transition);
}

.theme-btn:hover {
    transform: scale(1.1);
}

/* Hero Section */
.hero-section {
    margin: 30px 0;
}

.hero-image {
    height: 480px;
    background-size: cover;
    background-position: center;
    border-radius: 16px;
    display: flex;
    align-items: flex-end;
    overflow: hidden;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.08);
}

.hero-content {
    padding: 40px;
    color: #ffffff;
    max-width: 800px;
}

.category-badge {
    background-color: var(--primary);
    color: #ffffff;
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 700;
    border-radius: 4px;
    letter-spacing: 1px;
    display: inline-block;
    margin-bottom: 15px;
}

.hero-title {
    font-family: 'Playfair Display', serif;
    font-size: 38px;
    font-weight: 900;
    line-height: 1.2;
    margin-bottom: 15px;
}

.hero-excerpt {
    font-size: 16px;
    opacity: 0.9;
    margin-bottom: 20px;
    line-height: 1.6;
}

.hero-meta {
    font-size: 12px;
    opacity: 0.8;
}

/* News Grid Section */
.news-section {
    margin: 50px 0;
}

.section-title {
    font-family: 'Playfair Display', serif;
    font-size: 26px;
    font-weight: 700;
    margin-bottom: 24px;
    position: relative;
    padding-bottom: 8px;
}

.section-title::after {
    content: '';
    position: absolute;
    bottom: 0;
    left: 0;
    width: 60px;
    height: 3px;
    background-color: var(--primary);
}

.news-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 30px;
}

.news-card {
    background-color: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: hidden;
    box-shadow: 0 4px 15px rgba(0,0,0,0.02);
    transition: var(--transition);
}

.news-card:hover {
    transform: translateY(-5px);
    box-shadow: 0 10px 25px rgba(0,0,0,0.06);
    border-color: var(--primary);
}

.card-img {
    height: 200px;
    background-size: cover;
    background-position: center;
}

.card-body {
    padding: 24px;
}

.card-tag {
    color: var(--primary);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    display: block;
    margin-bottom: 8px;
}

.card-title {
    font-family: 'Playfair Display', serif;
    font-size: 20px;
    font-weight: 700;
    line-height: 1.4;
    margin-bottom: 12px;
    color: var(--text-color);
}

.card-text {
    font-size: 14px;
    color: var(--text-muted);
    margin-bottom: 15px;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
}

.card-date {
    font-size: 12px;
    color: var(--text-muted);
}

/* About Section */
.about-section {
    background-color: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 16px;
    padding: 40px;
    margin: 50px 0;
    transition: var(--transition);
}

.about-content h2 {
    font-family: 'Playfair Display', serif;
    font-size: 28px;
    margin-bottom: 15px;
}

.about-content p {
    color: var(--text-muted);
    margin-bottom: 15px;
}

.about-content .disclaimer {
    background-color: rgba(221, 107, 32, 0.08);
    border-left: 4px solid #dd6b20;
    padding: 16px;
    border-radius: 6px;
    color: #7b341e;
    font-size: 13px;
    margin-top: 20px;
}

/* Footer Section */
.site-footer {
    background-color: var(--header-bg);
    border-top: 1px solid var(--border);
    padding: 30px 0;
    text-align: center;
    transition: var(--transition);
}

/* Responsive Styles */
@media (max-width: 768px) {
    .site-header .container {
        flex-direction: column;
        gap: 15px;
        text-align: center;
    }
    
    .main-nav {
        flex-wrap: wrap;
        justify-content: center;
        gap: 8px;
        width: 100%;
    }
    
    .nav-separator {
        display: none !important;
    }
    
    .main-nav a {
        font-size: 13px;
        padding: 6px 10px;
    }
    
    .hero-section {
        margin: 20px 0 40px 0 !important;
    }
    
    .hero-image {
        min-height: 400px !important;
        height: auto !important;
        border-radius: 12px;
    }
    
    .hero-content {
        padding: 30px 16px !important;
    }
    
    .hero-title {
        font-size: 28px !important;
        line-height: 1.3 !important;
    }
    
    .hero-excerpt {
        font-size: 14.5px !important;
        margin-bottom: 25px !important;
    }
    
    .pricing-grid {
        grid-template-columns: 1fr;
        gap: 30px;
        margin-top: 30px;
    }
    
    .pricing-card {
        padding: 30px 20px;
    }
    
    .pricing-card.popular {
        transform: none !important;
        margin-top: 15px;
    }
    
    .pricing-card.popular::before {
        top: -12px;
        font-size: 10px;
    }
    
    .pricing-price {
        font-size: 36px;
    }
    
    .feature-grid {
        grid-template-columns: 1fr;
        gap: 20px;
    }
    
    .about-section {
        padding: 24px;
        margin: 30px 0;
    }
    
    .about-content h2 {
        font-size: 22px;
    }
}

/* TanishkaDeep SaaS Landing Page Inspired Styles */
.gradient-text {
    background: linear-gradient(135deg, var(--primary) 0%, #ff8c00 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    font-weight: 800;
}

/* Partner Logo Ticker */
.logo-ticker {
    padding: 30px 0;
    overflow: hidden;
    background: rgba(255, 255, 255, 0.01);
    border-top: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    margin: 40px 0;
}
.logo-ticker-title {
    text-align: center;
    font-size: 11px;
    font-weight: 700;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 2px;
    margin-bottom: 20px;
}
.logo-ticker-inner {
    display: flex;
    justify-content: space-around;
    align-items: center;
    flex-wrap: wrap;
    gap: 30px;
    opacity: 0.6;
}
.ticker-logo {
    font-weight: 800;
    font-size: 16px;
    letter-spacing: -0.5px;
    color: var(--text-muted);
    display: flex;
    align-items: center;
    gap: 8px;
}

/* Testimonials Section */
.testimonials-section {
    padding: 80px 0;
}
.testimonials-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 24px;
    margin-top: 40px;
}
.testimonial-card {
    background-color: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 16px;
    padding: 30px;
    box-shadow: 0 10px 30px -15px rgba(0, 0, 0, 0.05);
    transition: var(--transition);
    position: relative;
    overflow: hidden;
}
.testimonial-card:hover {
    transform: translateY(-5px);
    border-color: var(--primary);
    box-shadow: 0 15px 30px -10px rgba(255, 179, 0, 0.1);
}
.testimonial-quote {
    font-size: 14px;
    line-height: 1.7;
    color: var(--text-muted);
    font-style: italic;
    margin-bottom: 20px;
}
.testimonial-profile {
    display: flex;
    align-items: center;
    gap: 12px;
}
.testimonial-avatar {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: linear-gradient(135deg, var(--primary) 0%, #ffa000 100%);
    color: #000;
    display: flex;
    align-items: center;
    justify-content: center;
    font-weight: 800;
    font-size: 15px;
}
.testimonial-info h4 {
    font-size: 14.5px;
    font-weight: 700;
    color: var(--text-color);
}
.testimonial-info p {
    font-size: 11.5px;
    color: var(--text-muted);
}

/* FAQ Accordion Section */
.faq-section {
    padding: 80px 0;
    border-top: 1px solid var(--border);
}
.faq-list {
    max-width: 800px;
    margin: 40px auto 0 auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
}
.faq-item {
    background-color: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: hidden;
    transition: var(--transition);
}
.faq-item:hover {
    border-color: var(--primary);
}
.faq-question {
    padding: 20px 24px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    cursor: pointer;
    font-weight: 700;
    font-size: 15.5px;
    color: var(--text-color);
}
.faq-answer {
    max-height: 0;
    overflow: hidden;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    background-color: rgba(255, 255, 255, 0.01);
}
.faq-answer-inner {
    padding: 0 24px 20px 24px;
    font-size: 14px;
    line-height: 1.7;
    color: var(--text-muted);
}
.faq-item.active .faq-answer {
    max-height: 200px;
}
.faq-toggle-icon {
    font-size: 14px;
    transition: var(--transition);
    color: var(--text-muted);
}
.faq-item.active .faq-toggle-icon {
    transform: rotate(180deg);
    color: var(--primary);
}
}
`

const defaultCustomHomepageJS = `// Wait for DOM to load
document.addEventListener('DOMContentLoaded', () => {
    
    // 1. Dynamic Footer Year
    const yearSpan = document.getElementById('current-year');
    if (yearSpan) {
        yearSpan.textContent = new Date().getFullYear();
    }

    // 2. Beautiful Dark/Light Mode Theme Toggle
    const themeBtn = document.getElementById('theme-toggle-btn');
    
    // Check local storage for theme preference
    const savedTheme = localStorage.getItem('custom-theme') || 'light';
    if (themeBtn) {
        document.documentElement.setAttribute('data-theme', savedTheme);
        updateThemeButtonIcon(savedTheme);

        themeBtn.addEventListener('click', () => {
            const currentTheme = document.documentElement.getAttribute('data-theme');
            const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
            
            document.documentElement.setAttribute('data-theme', newTheme);
            localStorage.setItem('custom-theme', newTheme);
            updateThemeButtonIcon(newTheme);
            
            // Dynamic rotate micro-animation
            themeBtn.style.transform = 'rotate(360deg)';
            setTimeout(() => {
                themeBtn.style.transform = '';
            }, 300);
        });
    }

    function updateThemeButtonIcon(theme) {
        if (!themeBtn) return;
        if (theme === 'dark') {
            themeBtn.textContent = '☀️';
            themeBtn.setAttribute('title', 'Ganti ke Mode Terang');
        } else {
            themeBtn.textContent = '🌙';
            themeBtn.setAttribute('title', 'Ganti ke Mode Gelap');
        }
    }

    const saasUrl = window.SAAS_BACKEND_URL || 'https://meowcing.my.id';
    
    // Update all dynamic SaaS links with resolved SAAS_BACKEND_URL
    document.querySelectorAll('[href*="https://meowcing.my.id"]').forEach(el => {
        const path = el.getAttribute('href').replace('https://meowcing.my.id', '');
        el.setAttribute('href', saasUrl + path);
    });

    // 3. Dynamic Pricing Plans Fetch
    const pricingGrid = document.getElementById('pricing-plans-grid');
    if (pricingGrid) {
        fetch(saasUrl + '/api/v1/public/plans')
            .then(res => res.json())
            .then(plans => {
                const activePlans = plans.filter(p => p.price !== "0");
                if (activePlans.length > 0) {
                    pricingGrid.innerHTML = activePlans.map(plan => {
                        const isPopular = plan.is_popular;
                        const cardClass = isPopular ? 'pricing-card popular' : 'pricing-card';
                        const popularBadge = isPopular ? '<span class="popular-badge">PILIHAN TERBAIK</span>' : '';
                        const btnClass = isPopular ? 'cta-btn' : 'cta-btn secondary';
                        const featuresList = plan.features_list || JSON.parse(plan.features || '[]');
                        
                        const featuresHtml = featuresList.map(feature => {
                            const isBold = feature.includes('Aplikasi Android') || feature.includes('Semua Fitur') || feature.includes('Free Update') || feature.includes('Free Biaya') || feature.includes('Bebas Jumlah');
                            
                            // Check if feature contains "ongoing"
                            const badgeOngoing = feature.toLowerCase().includes('ongoing') || feature.toLowerCase().includes('on-going') ? 
                                ' <span class="badge-new" style="background: rgba(255, 179, 0, 0.15); color: var(--primary); font-size: 9px; padding: 2px 6px;">ON-GOING</span>' : '';
                                
                            const cleanText = feature.replace('(ON-GOING)', '').replace('ON-GOING', '');
                            const textSpan = isBold ? '<strong>' + cleanText + '</strong>' : cleanText;
                            
                            return '<li>' +
                                '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" style="color: var(--primary); flex-shrink: 0; margin-right: 2px;"><polyline points="20 6 9 17 4 12"></polyline></svg>' +
                                '<span>' + textSpan + badgeOngoing + '</span>' +
                            '</li>';
                        }).join('');

                        let ctaLink = plan.cta_link;
                        if (ctaLink.startsWith('/')) {
                            ctaLink = saasUrl + ctaLink + '?plan=' + plan.id;
                        }
                        
                        return '<div class="' + cardClass + '">' +
                                popularBadge +
                                '<div class="pricing-header">' +
                                    '<h3 style="font-size: 20px; font-weight: 700; font-family: \'Plus Jakarta Sans\', sans-serif; color: var(--text-color);">' + plan.name.toUpperCase() + '</h3>' +
                                    '<p style="font-size: 12px; color: var(--text-muted); margin-top: 5px;">' + plan.description + '</p>' +
                                    '<div class="pricing-price" style="font-size: 44px; font-weight: 800; color: var(--text-color); margin: 20px 0;">' +
                                        'Rp' + plan.price + '<span>/bulan</span>' +
                                    '</div>' +
                                '</div>' +
                                '<ul class="pricing-features" style="list-style: none; padding: 0; margin: 30px 0; text-align: left; flex-grow: 1;">' +
                                    featuresHtml +
                                '</ul>' +
                                '<a href="' + ctaLink + '" class="' + btnClass + '">' + (plan.cta_text || 'Sewa Sekarang') + '</a>' +
                            '</div>';
                    }).join('');
                }
            })
            .catch(err => {
                console.error('Gagal mengambil data paket real-time, menggunakan fallback static', err);
            });
    }

    // FAQ Accordion Toggle
    const faqItems = document.querySelectorAll('.faq-item');
    faqItems.forEach(item => {
        const question = item.querySelector('.faq-question');
        if (question) {
            question.addEventListener('click', () => {
                const isActive = item.classList.contains('active');
                
                faqItems.forEach(otherItem => {
                    otherItem.classList.remove('active');
                });
                
                if (!isActive) {
                    item.classList.add('active');
                }
            });
        }
    });
});`

const defaultCustomHomepageSubpageHTML = `<!DOCTYPE html>
<html lang="id">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sub-Page Kustom & Panduan Dinamis</title>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;600;700&display=swap" rel="stylesheet">
    <style>
        body {
            font-family: 'Outfit', sans-serif;
            background: #f8fafc;
            color: #1e293b;
            margin: 0;
            padding: 20px;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            padding: 30px;
            border-radius: 12px;
            box-shadow: 0 4px 6px -1px rgba(0,0,0,0.1);
        }
        h1 {
            color: #e63329;
            border-bottom: 2px solid #e2e8f0;
            padding-bottom: 10px;
        }
        .guide-box {
            background: #eff6ff;
            border-left: 4px solid #3b82f6;
            padding: 15px;
            border-radius: 6px;
            margin-bottom: 25px;
            font-size: 14px;
            color: #1e3a8a;
        }
        .code-block {
            background: #1e293b;
            color: #f8fafc;
            padding: 15px;
            border-radius: 8px;
            font-family: monospace;
            font-size: 12px;
            overflow-x: auto;
            margin: 10px 0;
        }
        .article-list {
            margin-top: 20px;
            display: flex;
            flex-direction: column;
            gap: 15px;
        }
        .article-item {
            padding: 15px;
            border: 1px solid #e2e8f0;
            border-radius: 8px;
            background: #f8fafc;
        }
        .article-title {
            margin: 0 0 5px 0;
            font-size: 18px;
            font-weight: 600;
        }
        .article-title a {
            color: #1e293b;
            text-decoration: none;
        }
        .article-title a:hover {
            color: #e63329;
        }
        .article-excerpt {
            margin: 0;
            font-size: 14px;
            color: #64748b;
        }
        .back-link {
            display: inline-block;
            margin-bottom: 20px;
            color: #e63329;
            text-decoration: none;
            font-weight: 600;
        }
    </style>
</head>
<body>
    <div class="container">
        <a href="/" class="back-link">← Kembali ke Beranda</a>
        <h1>📄 Panduan Integrasi Artikel Dinamis</h1>
        
        <div class="guide-box">
            <strong>💡 Panduan Developer:</strong><br>
            Halaman ini di-render menggunakan Go HTML Template. Anda dapat mencetak daftar artikel terbit terbaru secara dinamis dari database menggunakan variabel <code>.LatestArticles</code>.
            Berikut adalah contoh kode perulangan yang digunakan di halaman ini:
            <div class="code-block">
&#x7b;&#x7b;range .LatestArticles&#x7d;&#x7d;<br>
&nbsp;&nbsp;&lt;div class="article-item"&gt;<br>
&nbsp;&nbsp;&nbsp;&nbsp;&lt;h3&gt;&lt;a href="/artikel/&#x7b;&#x7b;.Slug&#x7d;&#x7b;"&gt;&#x7b;&#x7b;.Title&#x7d;&#x7d;&lt;/a&gt;&lt;/h3&gt;<br>
&nbsp;&nbsp;&nbsp;&nbsp;&lt;p&gt;&#x7b;&#x7b;.Excerpt&#x7d;&#x7d;&lt;/p&gt;<br>
&nbsp;&nbsp;&lt;/div&gt;<br>
&#x7b;&#x7b;else&#x7d;&#x7d;<br>
&nbsp;&nbsp;&lt;p&gt;Belum ada artikel.&lt;/p&gt;<br>
&#x7b;&#x7b;end&#x7d;&#x7d;
            </div>
        </div>

        <div class="article-list">
            <h2>🔴 Demo Live Artikel Terbaca (Dinamis dari Database):</h2>
            {{range .LatestArticles}}
            <div class="article-item">
                <h3 class="article-title"><a href="/artikel/{{.Slug}}">{{.Title}}</a></h3>
                <p class="article-excerpt">{{.Excerpt}}</p>
                <span style="font-size: 11px; color: #94a3b8;">Dipublikasikan pada: {{.CreatedAt.Format "02 Jan 2006"}}</span>
            </div>
            {{else}}
            <p>Belum ada artikel saat ini di database.</p>
            {{end}}
        </div>
    </div>
</body>
</html>`

const defaultCustomHomepageAboutHTML = `<!DOCTYPE html>
<html lang="id">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Tentang Kami</title>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;600;700&display=swap" rel="stylesheet">
    <style>
        body {
            font-family: 'Outfit', sans-serif;
            background: #f8fafc;
            color: #1e293b;
            margin: 0;
            padding: 20px;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            padding: 30px;
            border-radius: 12px;
            box-shadow: 0 4px 6px -1px rgba(0,0,0,0.1);
        }
        h1 {
            color: #3182ce;
            border-bottom: 2px solid #e2e8f0;
            padding-bottom: 10px;
        }
        .article-list {
            margin-top: 20px;
            display: flex;
            flex-direction: column;
            gap: 15px;
        }
        .article-item {
            padding: 15px;
            border: 1px solid #e2e8f0;
            border-radius: 8px;
            background: #f8fafc;
        }
        .article-title {
            margin: 0 0 5px 0;
            font-size: 18px;
            font-weight: 600;
        }
        .article-title a {
            color: #1e293b;
            text-decoration: none;
        }
        .article-title a:hover {
            color: #3182ce;
        }
        .article-excerpt {
            margin: 0;
            font-size: 14px;
            color: #64748b;
        }
        .back-link {
            display: inline-block;
            margin-bottom: 20px;
            color: #3182ce;
            text-decoration: none;
            font-weight: 600;
        }
    </style>
</head>
<body>
    <div class="container">
        <a href="/" class="back-link">← Kembali ke Beranda</a>
        <h1>ℹ️ Tentang Kami (Kustom Sub-Page)</h1>
        <p>Halaman ini diserve secara dinamis dari folder <code>/about-us/index.html</code> menggunakan Go HTML Template.</p>
        
        <div class="article-list">
            <h2>Berita Terkait Kami:</h2>
            {{range .LatestArticles}}
            <div class="article-item">
                <h3 class="article-title"><a href="/artikel/{{.Slug}}">{{.Title}}</a></h3>
                <p class="article-excerpt">{{.Excerpt}}</p>
            </div>
            {{else}}
            <p>Belum ada artikel saat ini.</p>
            {{end}}
        </div>
    </div>
</body>
</html>`

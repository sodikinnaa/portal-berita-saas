# Panduan Integrasi Deteksi Domain pada CLI `pbt` & Backend SaaS

Dokumen ini berisi kode sumber dan langkah-barang untuk memodifikasi CLI `pbt` (ditulis dalam Go) dan Laravel SaaS Backend (`wakdondin-member`) agar status instalasi tidak hanya mencatat IP VPS, melainkan juga mendeteksi dan menyimpan nama domain aktif (misal `siapdigital.id` atau `jatimaktual.id`) yang digunakan oleh setiap portal.

---

## Bagian 1: Deteksi Domain di CLI `pbt` (Bahasa Go)

Tambahkan fungsi deteksi domain ini ke kode sumber CLI `pbt` Anda sebelum mengirim data status ke backend. Ada dua metode deteksi yang andal:

### Metode A: Deteksi via Nginx Config (Sangat Direkomendasikan)
Metode ini membaca direktori `/etc/nginx/conf.d` untuk mencari berkas `.conf` yang melakukan `proxy_pass` ke port portal terkait, lalu mengambil nama `server_name`-nya. Metode ini sangat cepat karena tidak membutuhkan koneksi database.

```go
package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
)

// DetectDomainByPort mencari domain yang di-proxy ke port tertentu lewat Nginx
func DetectDomainByPort(port int) string {
	nginxConfDir := "/etc/nginx/conf.d"
	files, err := ioutil.ReadDir(nginxConfDir)
	if err != nil {
		return ""
	}

	proxyTarget := fmt.Sprintf("127.0.0.1:%d", port)
	proxyTargetAlt := fmt.Sprintf("localhost:%d", port)

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".conf") {
			continue
		}
		path := filepath.Join(nginxConfDir, file.Name())
		content, err := ioutil.ReadFile(path)
		if err != nil {
			continue
		}

		contentStr := string(content)
		// Cek apakah konfigurasi Nginx ini me-redirect ke port portal kita
		if strings.Contains(contentStr, proxyTarget) || strings.Contains(contentStr, proxyTargetAlt) {
			// Regex untuk mengambil nilai server_name
			re := regexp.MustCompile(`server_name\s+([^;]+);`)
			matches := re.FindStringSubmatch(contentStr)
			if len(matches) > 1 {
				domains := strings.Fields(matches[1])
				for _, dom := range domains {
					// Abaikan localhost, wildcard, atau domain default ip
					if dom != "" && dom != "localhost" && !strings.Contains(dom, "www.") && !regexp.MustCompile(`^[0-9\.]+$`).MatchString(dom) {
						return dom
					}
				}
				if len(domains) > 0 {
					return domains[0]
				}
			}
		}
	}
	return ""
}
```

### Metode B: Deteksi via Database Query (Sebagai Fallback)
Jika Nginx tidak terdeteksi, CLI bisa membaca langsung dari tabel database PostgreSQL instalasi portal yang bersangkutan.

```go
package main

import (
	"database/sql"
	"strings"

	_ "github.com/lib/pq"
)

// DetectDomainByDatabase mengambil nilai custom_domain langsung dari tabel site_settings portal
func DetectDomainByDatabase(databaseURL string) string {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return ""
	}
	defer db.Close()

	var domain string
	// Query mengambil data domain kustom yang tersimpan di portal
	err = db.QueryRow("SELECT value FROM site_settings WHERE key = 'custom_domain' LIMIT 1").Scan(&domain)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(domain)
}
```

### Cara Penerapan pada Payload CLI `pbt`:
Saat CLI menyusun data portal untuk dikirim ke API backend, jalankan fungsi deteksi di atas dan masukkan hasilnya ke field `domain`:

```go
// Contoh struktur data portal saat CLI mengirim status ping ke SaaS
type PortalStatusPayload struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	Database string `json:"database"`
	Status   string `json:"status"`
	Domain   string `json:"domain"` // Field domain baru
}

// Loop saat mendeteksi portal aktif
for _, p := range activePortals {
	domain := DetectDomainByPort(p.Port)
	if domain == "" {
		domain = DetectDomainByDatabase(p.DatabaseURL)
	}

	payload.Portals = append(payload.Portals, PortalStatusPayload{
		PID:      p.PID,
		Port:     p.Port,
		Path:     p.Path,
		Database: p.DatabaseURL,
		Status:   "running",
		Domain:   domain, // Domain berhasil dideteksi!
	})
}
```

### Cara Menampilkan Domain pada Output Perintah `pbt status` di Terminal:
Cari bagian kode CLI `pbt` Anda yang mencetak status daftar portal di terminal, panggil fungsi `DetectDomainByPort`, lalu sisipkan nama domainnya seperti ini:

```go
// Tampilan Output Terminal
domain := DetectDomainByPort(portal.Port)
fmt.Printf("  - [PORTAL] PID: %d | Domain: %s | Port: %d | Status: %s | Uptime: %s | Mem: %s\n", 
    portal.PID, 
    domain, 
    portal.Port, 
    portal.Status, 
    portal.Uptime, 
    portal.Mem,
)
```

---

## Bagian 2: Penerimaan Domain di Laravel SaaS Backend (`wakdondin-member`)

Lakukan pembaruan berikut pada repositori backend SaaS Anda agar dapat menerima dan menyimpan domain yang dikirim oleh CLI.

### Langkah 1: Buat Migration Database Baru
Jalankan di terminal backend SaaS Anda:
```bash
php artisan make:migration add_domain_to_portals_table
```

Isi berkas migrasinya:
```php
<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        // Menambahkan kolom domain ke tabel portals (atau vps_portals)
        Schema::table('portals', function (Blueprint $table) {
            $table->string('domain')->nullable()->after('port');
        });
    }

    public function down(): void
    {
        Schema::table('portals', function (Blueprint $table) {
            $table->dropColumn('domain');
        });
    }
};
```
Jalankan migrasi di server database SaaS Anda:
```bash
php artisan migrate
```

### Langkah 2: Perbarui Controller Penerima API
Buka controller yang menangani request `pbt status` (misalnya `VpsSyncController.php` atau `VpsController.php`), lalu perbarui logic penyimpanan data portal:

```php
<?php

namespace App\Http\Controllers\Api;

use App\Http\Controllers\Controller;
use Illuminate\Http\Request;
use App\Models\VpsInstallation;
use App\Models\Portal;

class VpsSyncController extends Controller
{
    public function syncStatus(Request $request)
    {
        $request->validate([
            'token' => 'required|string',
            'portals' => 'required|array',
            'portals.*.port' => 'required|integer',
            'portals.*.path' => 'required|string',
            'portals.*.pid' => 'required|integer',
            'portals.*.domain' => 'nullable|string', // Validasi domain kustom
            'portals.*.status' => 'required|string',
        ]);

        $vps = VpsInstallation::where('token', $request->token)->first();
        if (!$vps) {
            return response()->json(['message' => 'Token tidak valid'], 401);
        }

        // Update IP VPS utama
        $vps->update([
            'ip_address' => $request->ip(),
            'last_ping' => now(),
        ]);

        // Simpan / update domain masing-masing portal di VPS tersebut
        foreach ($request->portals as $portalData) {
            $vps->portals()->updateOrCreate(
                ['path' => $portalData['path']],
                [
                    'port' => $portalData['port'],
                    'pid' => $portalData['pid'],
                    'domain' => $portalData['domain'] ?? null, // Simpan nama domain
                    'status' => $portalData['status'],
                    'database_url' => $portalData['database'] ?? null,
                ]
            );
        }

        return response()->json(['message' => 'Status berhasil disinkronisasi']);
    }
}
```

### Langkah 3: Tampilkan Domain di Dashboard SaaS (Blade View)
Pada halaman daftar portal di dashboard admin SaaS Anda, Anda sekarang bisa menampilkan domainnya secara langsung:

```html
<td class="px-6 py-4">
    @if($portal->domain)
        <a href="https://{{ $portal->domain }}" target="_blank" class="text-blue-600 hover:underline font-semibold">
            {{ $portal->domain }}
        </a>
    @else
        <span class="text-gray-400">Belum terhubung ke domain (Hanya IP: {{ $vps->ip_address }}:{{ $portal->port }})</span>
    @endif
</td>
```

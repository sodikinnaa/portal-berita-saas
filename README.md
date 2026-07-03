# Porta Berita

MVP web portal berita berbasis Go dari export Elementor `elementor-36-2026-06-16.json`.

## Fitur

- HTML export Elementor di-embed ke binary Go, jadi tidak perlu baca file saat runtime.
- Semua fitur runtime memakai PostgreSQL lewat `DATABASE_URL`.
- Login/logout penulis dengan session cookie HTTP-only.
- Dashboard penulis untuk CRUD artikel.
- Artikel publik dari data store via slug (`/artikel/{slug}`).
- Draft/submitted/published/needs_revision/archived workflow.
- Upload media gambar lokal ke `/uploads`.
- Role dasar: `admin`, `editor`, `writer`.
- Graceful shutdown untuk deploy rolling/restart aman.
- Structured JSON logging dengan `log/slog`.
- Middleware security headers, panic recovery, dan access log.
- Route halaman:
  - `GET /` untuk home Elementor static
  - `GET /artikel` untuk daftar artikel publik
  - `GET /artikel/{slug}` untuk baca artikel dari data store
  - `GET /login` untuk login penulis
  - `GET /dashboard` untuk dashboard internal
- Health endpoints untuk load balancer/Kubernetes:
  - `GET /healthz`
  - `GET /readyz`

## Struktur

```txt
cmd/portal/                 entrypoint aplikasi
database/migrations/        migration SQL PostgreSQL ala Laravel
database/seeders/           seed data awal/demo PostgreSQL ala Laravel
internal/config/            konfigurasi environment
internal/httpserver/        routing, handler, middleware
internal/web/               template embedded
internal/web/templates/     HTML hasil export Elementor
```

## Menjalankan lokal

Jalankan PostgreSQL dulu. Untuk development lokal, copy `.env.example` ke `.env.local` lalu sesuaikan jika perlu.

CMD Windows:

```bat
copy .env.example .env.local
```

PowerShell:

```powershell
Copy-Item .env.example .env.local
```

Git Bash atau shell Linux/macOS:

```sh
cp .env.example .env.local
```

Run biasa:

```bat
go run ./cmd/portal
```

Run mode dev realtime/watch:

```bat
go run ./cmd/dev
```

`cmd/portal` otomatis membaca `.env` dan `.env.local`. Environment variable dari shell/Docker/server tetap prioritas tertinggi, sehingga production cukup inject env dari runtime tanpa file lokal.

Mode dev akan restart server otomatis saat file `.go`, `.html`, `.json`, `.css`, atau `.js` berubah.

Default listen di `:8080`. Override lewat environment variable.

CMD Windows:

```bat
set "ADDR=:8090" && set "DATABASE_URL=postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable" && go run ./cmd/portal
```

PowerShell:

```powershell
$env:ADDR=":8090"; $env:DATABASE_URL="postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable"; go run ./cmd/portal
```

Git Bash atau shell Linux/macOS:

```sh
ADDR=:8090 DATABASE_URL='postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' go run ./cmd/portal
```

Lalu buka:

```txt
http://localhost:8080
http://localhost:8080/artikel
```

## Build

```sh
go build -o portal.exe ./cmd/portal
```

## Docker

```sh
docker build -t porta-berita:latest .
```

App membutuhkan `DATABASE_URL`, jadi jalankan container dengan PostgreSQL. File `.env` dan `.env.local` tidak ikut Docker build; production harus inject env lewat `docker run -e`, compose, atau secret manager:

```sh
docker run --rm -p 8080:8080 \
  -e DATABASE_URL='postgres://portal:portal123@host.docker.internal:5432/portal_berita?sslmode=disable' \
  porta-berita:latest
```

### Docker Windows + PostgreSQL

Pastikan Docker Desktop sudah jalan sampai status **Engine running**. Cara yang disarankan adalah menjalankan app dan PostgreSQL dalam satu Docker network.

Buat network:

```sh
docker network create portal-network
```

Jalankan PostgreSQL:

```sh
docker run --name portal-postgres --network portal-network -e POSTGRES_USER=portal -e POSTGRES_PASSWORD=portal123 -e POSTGRES_DB=portal_berita -p 5432:5432 -d postgres:16
```

Build image app dari folder project:

```sh
docker build -t porta-berita:latest .
```

Run app di CMD Windows:

```bat
docker run --rm --name porta-berita ^
  --network portal-network ^
  -p 8080:8080 ^
  -e DATABASE_URL=postgres://portal:portal123@portal-postgres:5432/portal_berita?sslmode=disable ^
  porta-berita:latest
```

Run app di PowerShell:

```powershell
docker run --rm --name porta-berita `
  --network portal-network `
  -p 8080:8080 `
  -e DATABASE_URL="postgres://portal:portal123@portal-postgres:5432/portal_berita?sslmode=disable" `
  porta-berita:latest
```

Buka aplikasi di:

```txt
http://localhost:8080
```

Jika container PostgreSQL sudah pernah dibuat, jalankan ulang dengan:

```sh
docker start portal-postgres
```

Atau reset container PostgreSQL:

```sh
docker rm -f portal-postgres
```

Migration di `database/migrations` dan seed di `database/seeders` akan berjalan otomatis saat app pertama kali connect ke PostgreSQL.

## Konfigurasi

| Variable | Default | Keterangan |
| --- | --- | --- |
| `ADDR` | `:8080` | Address listen HTTP server |
| `APP_ENV` | `production` | Nama environment |
| `DEBUG` | `false` | Aktifkan debug log |
| `READ_TIMEOUT` | `5s` | Timeout baca request |
| `WRITE_TIMEOUT` | `10s` | Timeout tulis response |
| `IDLE_TIMEOUT` | `120s` | Keep-alive idle timeout |
| `SHUTDOWN_TIMEOUT` | `10s` | Timeout graceful shutdown |
| `SESSION_TTL` | `24h` | Durasi session login |
| `DATABASE_URL` | wajib diisi | Connection string PostgreSQL. App tidak akan start tanpa variable ini |
| `UPLOAD_DIR` | `uploads` | Folder upload media lokal |

Local bisa memakai `.env.local`; production/Docker sebaiknya memakai environment variable runtime, bukan file `.env`.

## PostgreSQL lokal

Cara cepat pakai Docker:

```bat
docker run --name portal-postgres -e POSTGRES_USER=portal -e POSTGRES_PASSWORD=portal123 -e POSTGRES_DB=portal_berita -p 5432:5432 -d postgres:16
```

Kalau container sudah pernah dibuat, jalankan ulang:

```bat
docker start portal-postgres
```

Jalankan app dengan PostgreSQL.

CMD Windows:

```bat
set "DATABASE_URL=postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable" && go run ./cmd/dev
```

PowerShell:

```powershell
$env:DATABASE_URL="postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable"; go run ./cmd/dev
```

Git Bash atau shell Linux/macOS:

```sh
DATABASE_URL='postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' go run ./cmd/dev
```

### Menjalankan Go dengan PostgreSQL + migration otomatis

Jika `DATABASE_URL` diisi, aplikasi Go akan otomatis connect ke PostgreSQL, menjalankan migration dari `database/migrations`, menjalankan seeder dari `database/seeders` jika tabel `users` masih kosong, lalu start server.

Dashboard/admin juga memakai store yang sama. Jadi login, session, kategori, artikel, review, publish, dan media metadata semuanya masuk PostgreSQL.

Di log startup, pastikan muncul:

```json
"msg":"using postgres store"
```

Kalau `DATABASE_URL` kosong atau salah, aplikasi akan gagal start dengan error `DATABASE_URL is required` atau `open postgres store`.

PowerShell:

```powershell
$env:DATABASE_URL="postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable"; go run ./cmd/portal
```

CMD Windows:

```bat
set "DATABASE_URL=postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable" && go run ./cmd/portal
```

Git Bash atau shell Linux/macOS:

```sh
DATABASE_URL='postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' go run ./cmd/portal
```

Cek migration yang sudah jalan:

```sh
docker exec -it portal-postgres psql -U portal -d portal_berita
```

Di dalam `psql`:

```sql
\dt
SELECT * FROM schema_migrations ORDER BY version;
SELECT id, email, role FROM users;
```

Saat pertama kali connect, app otomatis menjalankan file di `database/migrations`, lalu mengisi seed dari `database/seeders` jika tabel `users` masih kosong.

Penjelasan pemula ada di `docs/postgresql.md`. Detail struktur migration/seeder ada di `database/README.md`.

## Akun demo

| Role | Email | Password |
| --- | --- | --- |
| Admin | `admin@portal.test` | `admin123` |
| Writer | `writer@portal.test` | `writer123` |

## Test

```sh
go test ./...
```

Test fitur CMS ada di `internal/cms/store_test.go`:

1. Auth & login penulis
2. Manajemen artikel
3. Publikasi artikel by slug
4. Media gambar
5. Admin/review/role

## Catatan scaling jutaan traffic

App ini dibuat stateless dan sangat ringan karena halaman utama berupa template embedded tanpa database call. Untuk menerima traffic besar secara aman, jalankan beberapa instance di belakang:

1. CDN untuk cache asset/HTML publik.
2. Reverse proxy seperti Nginx/Caddy/Traefik.
3. Load balancer dengan health check ke `/healthz` atau `/readyz`.
4. Autoscaling container/VM berdasarkan CPU, latency, dan request rate.
5. Observability: log aggregation, metrics, alerting, dan tracing kalau nanti ada API/database.

Untuk MVP ini, bottleneck utama biasanya bukan kode Go-nya, tapi jaringan, CDN/cache policy, ukuran halaman, dan kapasitas server/load balancer.

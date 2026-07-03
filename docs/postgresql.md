# PostgreSQL untuk Portal Berita

Dokumen ini penjelasan singkat buat pemula tentang PostgreSQL dan cara project ini memakainya.

## PostgreSQL itu apa?

PostgreSQL adalah database server relasional. Artinya data disimpan dalam tabel yang punya baris dan kolom, mirip spreadsheet, tapi jauh lebih kuat untuk aplikasi web production.

Contoh tabel:

```txt
users
- id
- name
- email
- password_hash
- role

articles
- id
- author_id
- title
- slug
- content
- status
- published_at
```

Kalau JSON file itu satu file besar, PostgreSQL adalah service terpisah yang bisa menerima banyak koneksi aplikasi secara aman.

## Cara kerja di project ini

Saat app jalan:

```txt
Browser → Go App → PostgreSQL
```

Flow detail:

1. User buka halaman artikel.
2. Go handler di `internal/httpserver` minta data ke store.
3. `cmd/portal` wajib membuka `cms.PostgresStore` memakai `DATABASE_URL`.
4. `PostgresStore` menjalankan query SQL ke PostgreSQL.
5. Hasil dari PostgreSQL diubah ke struct Go seperti `Article`, `User`, `Category`.
6. Go render HTML response ke browser.

Kalau `DATABASE_URL` kosong, app tidak akan start. Semua fitur runtime seperti login, dashboard, kategori, artikel, review, publish, session, dan metadata media memakai PostgreSQL.

## Apa itu `DATABASE_URL`?

`DATABASE_URL` adalah alamat koneksi database.

Format umum:

```txt
postgres://USER:PASSWORD@HOST:PORT/NAMA_DATABASE?sslmode=disable
```

Contoh lokal:

```txt
postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable
```

Artinya:

```txt
USER          portal
PASSWORD      portal123
HOST          localhost
PORT          5432
DATABASE      portal_berita
SSL           disable untuk lokal
```

## Menjalankan PostgreSQL lokal pakai Docker

Buat container PostgreSQL:

```sh
docker run --name portal-postgres \
  -e POSTGRES_USER=portal \
  -e POSTGRES_PASSWORD=portal123 \
  -e POSTGRES_DB=portal_berita \
  -p 5432:5432 \
  -d postgres:16
```

Cek container jalan:

```sh
docker ps
```

Jalankan app:

```sh
DATABASE_URL='postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' go run ./cmd/dev
```

## Tabel yang dibuat otomatis

Project ini otomatis menjalankan migration di `database/migrations` saat connect pertama kali:

| Tabel | Fungsi |
| --- | --- |
| `users` | akun admin/editor/writer |
| `categories` | kategori artikel |
| `articles` | artikel berita |
| `sessions` | session login |
| `media` | metadata upload gambar |

Data gambar fisiknya tetap disimpan di folder `uploads`, bukan di PostgreSQL. Database hanya menyimpan metadata dan URL-nya.

## Kenapa PostgreSQL lebih cocok dari JSON untuk production?

PostgreSQL punya:

- indexing agar pencarian data cepat
- transaksi agar data aman saat banyak operasi bersamaan
- locking/concurrency control agar banyak request bisa jalan bareng
- backup/restore lebih proper
- bisa dipakai banyak instance app sekaligus

Untuk portal berita traffic besar, biasanya kombinasinya:

```txt
Cloudflare/CDN → Go App → PostgreSQL
```

CDN menangani mayoritas request baca publik, PostgreSQL menyimpan data utama secara aman.

## Perintah debugging dasar

Masuk ke PostgreSQL container:

```sh
docker exec -it portal-postgres psql -U portal -d portal_berita
```

Lihat tabel:

```sql
\dt
```

Lihat 5 artikel terbaru:

```sql
SELECT id, title, status, created_at FROM articles ORDER BY created_at DESC LIMIT 5;
```

Keluar dari `psql`:

```sql
\q
```

## Catatan production

Untuk production jangan pakai password contoh. Pakai database managed seperti:

- Supabase PostgreSQL
- Neon
- Railway PostgreSQL
- DigitalOcean Managed PostgreSQL
- AWS RDS PostgreSQL

Simpan `DATABASE_URL` di environment variable server, jangan hardcode di kode.

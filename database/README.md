# Database

Struktur folder ini dibuat mirip Laravel:

```txt
database/
├── migrations/   # perubahan struktur tabel, dijalankan berurutan
└── seeders/      # data awal/demo, dijalankan saat database masih kosong
```

Saat aplikasi dijalankan dengan `DATABASE_URL`, file SQL di folder ini akan di-embed ke binary Go dan dijalankan otomatis:

1. Aplikasi membuat tabel `schema_migrations`.
2. Semua file `database/migrations/*.sql` dijalankan berurutan berdasarkan nama file.
3. Versi migration yang sudah dijalankan disimpan di `schema_migrations`.
4. Jika tabel `users` masih kosong, semua file `database/seeders/*.sql` dijalankan berurutan.

## Menjalankan PostgreSQL lokal

```sh
docker run --name portal-postgres \
  -e POSTGRES_USER=portal \
  -e POSTGRES_PASSWORD=portal123 \
  -e POSTGRES_DB=portal_berita \
  -p 5432:5432 \
  -d postgres:16
```

Jalankan aplikasi dengan PostgreSQL:

```sh
DATABASE_URL='postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' go run ./cmd/portal
```

## Import manual lewat psql

Kalau ingin import manual tanpa menjalankan aplikasi, jalankan migration dulu lalu seeder:

```sh
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/migrations/202606170001_create_users_table.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/migrations/202606170002_create_categories_table.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/migrations/202606170003_create_articles_table.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/migrations/202606170004_create_sessions_table.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/migrations/202606170005_create_media_table.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/seeders/001_users.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/seeders/002_categories.sql
psql 'postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable' -f database/seeders/003_articles.sql
```

## Akun seed

| Role | Email | Password |
| --- | --- | --- |
| Admin | `admin@portal.test` | `admin123` |
| Writer | `writer@portal.test` | `writer123` |

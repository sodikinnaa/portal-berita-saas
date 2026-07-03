# PRD — Publikasi & Halaman Baca Artikel

## Ringkasan

Fitur ini membuat artikel dari database dapat dibaca oleh publik melalui URL slug. Halaman `/artikel/{slug}` menggantikan konten hardcoded dengan data aktual dari database.

## Tujuan

- Artikel published dapat diakses publik.
- Artikel draft tidak bisa diakses publik.
- URL artikel SEO-friendly berbasis slug.
- Halaman baca artikel menampilkan layout yang nyaman.
- Metadata artikel tersedia untuk SEO dasar.

## Non-Goals MVP

- Komentar pembaca.
- Paywall.
- Recommendation engine otomatis.
- AMP.
- Full-text search advanced.

## Target User

- Pembaca portal berita.
- Penulis yang ingin melihat hasil publish.
- Search engine crawler.

## User Story

- Sebagai pembaca, saya ingin membuka artikel via slug agar bisa membaca berita lengkap.
- Sebagai penulis, saya ingin artikel published saya tampil di publik.
- Sebagai sistem, saya harus menyembunyikan draft dari publik.

## Scope MVP

### Route Publik

- `GET /artikel/{slug}` — halaman baca artikel.
- `GET /artikel` — optional redirect ke artikel contoh atau daftar artikel.
- `GET /` — home menampilkan sebagian artikel published terbaru.

### Data yang Ditampilkan

- Title.
- Excerpt/dek.
- Author name.
- Published date.
- Category.
- Hero image.
- Content.
- Tags optional.
- Related articles optional/static MVP.

## Query Rules

- Cari artikel berdasarkan slug.
- Artikel harus `status = published`.
- Jika tidak ditemukan, tampilkan 404.
- Jika draft, tampilkan 404 untuk publik.

## SEO MVP

- `<title>` dari article title.
- `<meta name="description">` dari excerpt.
- Canonical URL optional.
- Open Graph basic optional.

## Acceptance Criteria

- `/artikel/{slug}` menampilkan data artikel dari database.
- Draft tidak dapat diakses publik.
- Slug tidak ditemukan menghasilkan halaman 404.
- Konten HTML/Markdown dirender aman.
- Home page bisa menautkan ke artikel via slug.

## Error State

- Artikel tidak ditemukan.
- Artikel draft.
- Slug invalid.
- DB error.
- Content kosong.

## Security

- Sanitasi HTML jika content dibuat dari rich text editor.
- Jika Markdown, render menjadi HTML dengan sanitizer.
- Jangan render script berbahaya dari user input.

## Prioritas

P0 karena ini inti konversi dari static HTML menjadi portal berita database-driven.

## Estimasi

- MVP: 0.5–1 hari setelah manajemen artikel selesai.
- SEO + related articles proper: 1–2 hari tambahan.

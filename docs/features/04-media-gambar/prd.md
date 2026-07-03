# PRD — Media & Gambar Artikel

## Ringkasan

Fitur ini memungkinkan penulis menambahkan gambar utama artikel. Untuk MVP, gambar dapat berupa URL eksternal atau upload lokal sederhana. Pada fase production, media dapat dipindahkan ke object storage seperti S3, Cloudflare R2, atau storage lain.

## Tujuan

- Penulis dapat memasang hero image pada artikel.
- Sistem menyimpan referensi gambar di database.
- Gambar tampil di halaman artikel dan listing.
- Validasi ukuran dan format gambar tersedia.

## Non-Goals MVP

- Media library kompleks.
- Image cropping UI.
- Automatic CDN transform.
- Multi-size responsive image generation.
- Bulk upload.

## Target User

- Penulis.
- Editor.
- Admin.

## User Story

- Sebagai penulis, saya ingin memasang gambar utama agar artikel lebih menarik.
- Sebagai editor, saya ingin gambar valid agar tampilan publik konsisten.
- Sebagai sistem, saya ingin menolak file berbahaya atau terlalu besar.

## Scope MVP Opsi A — URL Image

- Field `hero_image_url` pada form artikel.
- Validasi URL dasar.
- Render gambar jika URL ada.
- Gunakan placeholder jika URL kosong.

## Scope MVP Opsi B — Upload Lokal

- `POST /dashboard/media/upload`.
- Simpan file ke folder `uploads/`.
- Simpan metadata ke DB.
- Serve static file dari `/uploads/{filename}`.

## Database Schema Optional

### `media`

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `id` | integer/uuid | yes | Primary key |
| `owner_id` | FK users.id | yes | Uploader |
| `filename` | varchar | yes | Nama file storage |
| `original_name` | varchar | yes | Nama file asli |
| `mime_type` | varchar | yes | Jenis file |
| `size_bytes` | integer | yes | Ukuran file |
| `url` | text | yes | URL akses publik |
| `created_at` | timestamp | yes | Waktu upload |

## Validasi Upload

- Format: jpg, jpeg, png, webp.
- Max size MVP: 2MB–5MB.
- Filename digenerate random/uuid.
- Jangan gunakan nama file asli sebagai storage filename.
- MIME type dicek dari content, bukan hanya extension.

## Acceptance Criteria

- Artikel bisa menyimpan hero image URL.
- Jika gambar kosong, template memakai placeholder.
- Jika upload lokal dipakai, file bisa diakses via URL.
- File non-image ditolak.
- File terlalu besar ditolak.

## Security

- Jangan izinkan upload `.php`, `.exe`, `.js`, `.html`.
- Random filename.
- Batasi ukuran request.
- Jangan expose path filesystem asli.
- Untuk production, prefer object storage + CDN.

## Prioritas

P1. Bisa setelah artikel DB dan login selesai.

## Estimasi

- URL image MVP: 0.25 hari.
- Upload lokal MVP: 0.5–1 hari.
- Object storage production: 1–2 hari.

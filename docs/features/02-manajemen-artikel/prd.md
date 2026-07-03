# PRD — Manajemen Artikel

## Ringkasan

Fitur ini memungkinkan penulis membuat, mengedit, menyimpan draft, dan menghapus artikel dari dashboard. Artikel disimpan di database dan akan menjadi sumber data untuk halaman publik.

## Tujuan

- Penulis dapat membuat artikel baru.
- Penulis dapat menyimpan artikel sebagai draft.
- Penulis dapat mengedit artikel miliknya.
- Penulis dapat melihat daftar artikel miliknya.
- Sistem menyimpan slug artikel secara unik.

## Non-Goals MVP

- Editor approval workflow lengkap.
- Versioning/revision history.
- Collaborative editing realtime.
- Rich text editor kompleks seperti Gutenberg.
- Scheduled publish.

## Target User

- Writer/Penulis.
- Editor.
- Admin.

## User Story

- Sebagai penulis, saya ingin membuat artikel supaya berita saya bisa dipublikasikan.
- Sebagai penulis, saya ingin menyimpan draft supaya bisa melanjutkan nanti.
- Sebagai penulis, saya ingin melihat artikel saya supaya mudah mengelola konten.
- Sebagai admin, saya ingin bisa melihat semua artikel.

## Scope MVP

### Halaman Dashboard

- `GET /dashboard/articles` — daftar artikel.
- `GET /dashboard/articles/new` — form artikel baru.
- `POST /dashboard/articles` — simpan artikel baru.
- `GET /dashboard/articles/{id}/edit` — form edit artikel.
- `POST /dashboard/articles/{id}` — update artikel.
- `POST /dashboard/articles/{id}/delete` — hapus artikel.

### Field Artikel

- `title`
- `slug`
- `excerpt`
- `content`
- `category_id` atau `category_name` untuk MVP
- `hero_image_url`
- `status`: `draft`, `published`
- `author_id`

## Database Schema

### `articles`

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `id` | integer/uuid | yes | Primary key |
| `author_id` | FK users.id | yes | Penulis artikel |
| `title` | varchar | yes | Judul artikel |
| `slug` | varchar unique | yes | URL artikel |
| `excerpt` | text | no | Ringkasan artikel |
| `content` | text | yes | Isi artikel HTML/Markdown |
| `category` | varchar | no | Kategori MVP |
| `hero_image_url` | text | no | URL gambar utama |
| `status` | varchar | yes | `draft`, `published` |
| `published_at` | timestamp | no | Waktu publish |
| `created_at` | timestamp | yes | Waktu dibuat |
| `updated_at` | timestamp | yes | Waktu update |

## Validasi

- Title wajib minimal 5 karakter.
- Content wajib minimal 50 karakter untuk publish.
- Slug otomatis dari title jika kosong.
- Slug harus unik.
- Status harus `draft` atau `published`.
- Penulis hanya boleh edit artikel miliknya sendiri.
- Admin boleh edit semua artikel.

## Permission

| Role | Create | Edit Own | Edit All | Delete Own | Delete All |
| --- | --- | --- | --- | --- | --- |
| writer | yes | yes | no | yes | no |
| editor | yes | yes | yes | no/optional | no/optional |
| admin | yes | yes | yes | yes | yes |

## Acceptance Criteria

- Penulis login bisa membuat artikel draft.
- Artikel draft tidak muncul di halaman publik.
- Artikel published muncul di halaman publik.
- Slug artikel unik dan bisa dibuka via `/artikel/{slug}`.
- Penulis tidak bisa mengedit artikel penulis lain.
- Admin bisa melihat semua artikel.

## Error State

- Title kosong.
- Content kosong.
- Slug sudah dipakai.
- Artikel tidak ditemukan.
- User tidak punya akses.
- DB error saat simpan.

## Prioritas

P0 untuk CMS karena ini fitur inti.

## Estimasi

- MVP: 1–2 hari.
- Proper dengan editor UI lebih nyaman: 3–5 hari.

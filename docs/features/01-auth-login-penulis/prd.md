# PRD — Auth & Login Penulis

## Ringkasan

Fitur ini menyediakan autentikasi untuk penulis, admin, dan editor agar dapat mengakses dashboard internal portal berita. Login menjadi fondasi untuk fitur manajemen artikel, review, dan publikasi.

## Tujuan

- Penulis dapat login/logout dengan aman.
- User yang belum login tidak bisa mengakses dashboard.
- Sistem mengenali identitas dan role user.
- Session tetap aktif sesuai durasi yang ditentukan.
- Password disimpan menggunakan hash yang aman.

## Non-Goals MVP

- Social login Google/Facebook.
- Email verification.
- Forgot password via email.
- Two-factor authentication.
- SSO enterprise.

## Target User

- Penulis: membuat dan mengelola artikel sendiri.
- Editor: meninjau artikel penulis.
- Admin: mengelola semua user dan artikel.

## User Story

- Sebagai penulis, saya ingin login agar bisa masuk dashboard.
- Sebagai penulis, saya ingin logout agar akun saya aman setelah selesai bekerja.
- Sebagai admin, saya ingin role user tersimpan agar akses tiap user bisa dibatasi.
- Sebagai sistem, saya ingin menolak akses dashboard untuk user yang belum login.

## Scope MVP

### Halaman

- `GET /login` — form login.
- `POST /login` — proses autentikasi.
- `POST /logout` — hapus session.
- `GET /dashboard` — halaman setelah login.

### Field Login

- `email`
- `password`

### Session

- Cookie HTTP-only.
- Secure cookie aktif di production.
- SameSite Lax/Strict.
- Expired session default 24 jam.

### Role

Role awal:

- `admin`
- `editor`
- `writer`

## Database Schema

### `users`

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `id` | integer/uuid | yes | Primary key |
| `name` | varchar | yes | Nama user |
| `email` | varchar unique | yes | Email login |
| `password_hash` | text | yes | Hash password |
| `role` | varchar | yes | `admin`, `editor`, `writer` |
| `status` | varchar | yes | `active`, `inactive` |
| `created_at` | timestamp | yes | Waktu dibuat |
| `updated_at` | timestamp | yes | Waktu update |

### `sessions`

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `id` | varchar | yes | Session token hash/id |
| `user_id` | FK users.id | yes | Pemilik session |
| `expires_at` | timestamp | yes | Batas aktif session |
| `created_at` | timestamp | yes | Waktu session dibuat |

## Validasi

- Email wajib valid.
- Password wajib diisi.
- User harus `active`.
- Password dibandingkan dengan hash.
- Error login tidak boleh membocorkan apakah email atau password yang salah.

## Security Requirement

- Password hashing menggunakan `bcrypt` atau `argon2id`.
- Cookie `HttpOnly`.
- Cookie `Secure` saat production HTTPS.
- Regenerate session setelah login.
- Rate limit login minimal per IP/email di fase production.
- Jangan log password atau session token mentah.

## Acceptance Criteria

- User valid bisa login dan diarahkan ke `/dashboard`.
- User invalid mendapat pesan error umum.
- User belum login yang akses `/dashboard` diarahkan ke `/login`.
- Logout menghapus session dan redirect ke `/login`.
- Role user tersedia di context request.
- Password tidak pernah tersimpan dalam plaintext.

## Error State

- Email/password kosong.
- Credential salah.
- Akun inactive.
- Session expired.
- Database unavailable.

## Prioritas

P0 untuk MVP CMS karena semua fitur internal bergantung pada login.

## Estimasi

- MVP: 0.5–1 hari.
- Proper dengan rate limit + CSRF: 1–2 hari.

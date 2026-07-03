# PRD — API Key Admin, Profile User, dan Manajemen Writer

## Ringkasan

Fitur ini menambahkan kemampuan admin untuk membuat API key yang dapat dipakai oleh sistem eksternal untuk membuat artikel dan mengunggah media. Setiap aktivitas API key harus tercatat dengan identitas admin pembuat key. Fitur ini juga menambahkan pengaturan profil user dan manajemen writer oleh admin.

## Tujuan

- Admin dapat membuat, melihat, menonaktifkan, dan menghapus API key.
- API key dapat dipakai untuk membuat artikel melalui API.
- API key dapat dipakai untuk upload foto atau menyimpan URL foto untuk kebutuhan artikel.
- Artikel/media yang dibuat lewat API key memiliki identitas admin pembuat key sebagai actor/auditor.
- User dapat mengatur profil sendiri, termasuk nama, data personal, dan foto profil.
- Admin dapat menambahkan dan menghapus writer.
- Semua fitur runtime memakai PostgreSQL.

## Non-Goals MVP

- API key OAuth2/JWT kompleks.
- Fine-grained permission per field artikel.
- Billing/quota API per client.
- Public self-registration writer.
- Team/organization multi-tenant.
- Audit log visual lengkap di dashboard.
- Rotasi otomatis API key terjadwal.

## Target User

- Admin portal.
- Writer/penulis.
- Integrasi eksternal yang diberi API key oleh admin.

## Role & Permission

### Admin

- Generate API key.
- Melihat daftar API key miliknya atau semua API key, sesuai kebijakan dashboard admin.
- Revoke/delete API key.
- Membuat writer baru.
- Menghapus atau menonaktifkan writer.
- Mengubah role/status writer.

### Writer

- Mengubah profil sendiri.
- Mengubah nama dan data profil sendiri.
- Mengubah foto profil sendiri.
- Tidak dapat generate API key.
- Tidak dapat menambah/menghapus writer lain.

### API Client

- Mengakses endpoint API menggunakan API key.
- Hanya bisa melakukan aksi sesuai scope API key.
- Tidak bisa login dashboard.
- Tidak bisa mengubah profil user.

## Scope MVP

### 1. Admin Generate API Key

Admin dapat membuat API key dari dashboard.

Field input:

| Field | Required | Keterangan |
| --- | --- | --- |
| `name` | yes | Nama key, misal `Zapier Import`, `Mobile CMS`, `Partner Feed` |
| `scopes` | yes | Daftar permission API key |
| `expires_at` | no | Tanggal kadaluarsa opsional |

Scope MVP:

| Scope | Fungsi |
| --- | --- |
| `articles:create` | Membuat artikel |
| `media:upload` | Upload file foto |
| `media:url` | Menyimpan URL foto eksternal |

Output setelah generate:

- API key plaintext hanya ditampilkan sekali.
- Setelah user meninggalkan halaman, key tidak bisa dilihat lagi.
- Database hanya menyimpan hash API key.

Format key rekomendasi:

```txt
pk_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
pk_test_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Untuk MVP boleh memakai satu prefix:

```txt
portal_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 2. API Key untuk Post Artikel

Endpoint API:

```txt
POST /api/v1/articles
Authorization: Bearer {API_KEY}
```

Body JSON minimal:

```json
{
  "title": "Judul Artikel",
  "slug": "judul-artikel",
  "excerpt": "Ringkasan artikel",
  "content": "Isi artikel",
  "category": "Teknologi",
  "hero_image_url": "/uploads/example.jpg",
  "status": "draft"
}
```

Aturan:

- API key harus valid, aktif, belum expired, dan punya scope `articles:create`.
- Artikel yang dibuat lewat API key wajib menyimpan:
  - `created_by_api_key_id`
  - `api_actor_admin_id` atau `created_by_admin_id`
  - `author_id` default ke admin pembuat key atau writer yang ditentukan oleh kebijakan MVP.
- Untuk MVP, `author_id` diset ke admin pembuat API key agar identitas pembuat jelas.
- Status default `draft`.
- Jika API key tidak punya hak publish langsung, status `published` dari request diturunkan menjadi `draft` atau ditolak.

### 3. API Key untuk Upload Foto atau URL Foto

Endpoint upload file:

```txt
POST /api/v1/media/upload
Authorization: Bearer {API_KEY}
Content-Type: multipart/form-data
```

Field:

| Field | Required | Keterangan |
| --- | --- | --- |
| `file` | yes | File gambar |

Endpoint simpan URL eksternal:

```txt
POST /api/v1/media/url
Authorization: Bearer {API_KEY}
```

Body JSON:

```json
{
  "url": "https://example.com/image.jpg",
  "original_name": "image.jpg"
}
```

Aturan:

- Upload file butuh scope `media:upload`.
- Simpan URL butuh scope `media:url`.
- Metadata media menyimpan:
  - `owner_id` = admin pembuat API key
  - `created_by_api_key_id`
  - `source` = `upload` atau `external_url`
- File fisik tetap disimpan di `UPLOAD_DIR`.
- Database menyimpan metadata dan URL akses.

### 4. User Mengatur Profile

Route dashboard:

```txt
GET /dashboard/profile
POST /dashboard/profile
POST /dashboard/profile/avatar
```

Field profile MVP:

| Field | Required | Keterangan |
| --- | --- | --- |
| `name` | yes | Nama tampilan user |
| `bio` | no | Bio singkat |
| `phone` | no | Nomor telepon |
| `avatar_url` | no | URL foto profil |

Aturan:

- User hanya bisa mengubah profil dirinya sendiri.
- Email tidak diubah pada MVP kecuali admin yang melakukan perubahan.
- Foto profil bisa berupa upload file atau URL yang sudah tersedia.
- Validasi ukuran dan tipe file avatar.

### 5. Admin Menambahkan dan Menghapus Writer

Route dashboard:

```txt
GET /dashboard/users
GET /dashboard/users/new
POST /dashboard/users
POST /dashboard/users/{id}/delete
POST /dashboard/users/{id}/status
```

Field create writer:

| Field | Required | Keterangan |
| --- | --- | --- |
| `name` | yes | Nama writer |
| `email` | yes | Email unik |
| `password` | yes | Password awal |
| `status` | yes | `active` atau `inactive` |

Aturan:

- Hanya admin yang dapat membuat writer.
- Email writer harus unik.
- Password disimpan sebagai hash bcrypt.
- Delete writer MVP sebaiknya soft delete atau set `status=inactive` agar artikel lama tidak kehilangan author.
- Hard delete hanya boleh jika writer belum punya artikel/media/session.

## Database Update

### `users`

Tambahan field:

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `bio` | text | no | Bio user |
| `phone` | text | no | Nomor telepon |
| `avatar_url` | text | no | Foto profil user |
| `deleted_at` | timestamptz | no | Soft delete optional |

### `api_keys`

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `id` | text | yes | Primary key |
| `admin_id` | text FK users.id | yes | Admin pembuat key |
| `name` | text | yes | Nama key |
| `key_prefix` | text | yes | Prefix untuk identifikasi, bukan secret penuh |
| `key_hash` | text | yes | Hash API key |
| `scopes` | text[] atau jsonb | yes | Permission API key |
| `status` | text | yes | `active`, `revoked` |
| `last_used_at` | timestamptz | no | Terakhir dipakai |
| `expires_at` | timestamptz | no | Expired optional |
| `created_at` | timestamptz | yes | Waktu dibuat |
| `updated_at` | timestamptz | yes | Waktu update |
| `revoked_at` | timestamptz | no | Waktu revoke |

### `articles`

Tambahan field:

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `created_by_api_key_id` | text FK api_keys.id | no | Terisi jika artikel dibuat via API key |
| `api_actor_admin_id` | text FK users.id | no | Admin pemilik key saat artikel dibuat |

### `media`

Tambahan field:

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `created_by_api_key_id` | text FK api_keys.id | no | Terisi jika media dibuat via API key |
| `source` | text | yes | `upload`, `external_url`, `dashboard_upload` |

## API Contract MVP

### Generate API Key

```txt
POST /dashboard/api-keys
```

Form fields:

```txt
name=Partner Import
scopes=articles:create,media:upload,media:url
expires_at=2026-12-31
```

Response dashboard menampilkan plaintext API key sekali.

### Revoke API Key

```txt
POST /dashboard/api-keys/{id}/revoke
```

### Create Article via API

```txt
POST /api/v1/articles
Authorization: Bearer portal_xxx
Content-Type: application/json
```

### Upload Media via API

```txt
POST /api/v1/media/upload
Authorization: Bearer portal_xxx
Content-Type: multipart/form-data
```

### Save External Media URL via API

```txt
POST /api/v1/media/url
Authorization: Bearer portal_xxx
Content-Type: application/json
```

### Update Own Profile

```txt
GET /dashboard/profile
POST /dashboard/profile
POST /dashboard/profile/avatar
```

### Manage Writer

```txt
GET /dashboard/users
POST /dashboard/users
POST /dashboard/users/{id}/delete
```

## Acceptance Criteria

### API Key

- Admin dapat membuat API key dengan nama dan scope.
- API key plaintext hanya tampil sekali setelah generate.
- Database hanya menyimpan hash API key.
- API key revoked/expired tidak bisa dipakai.
- API key tanpa scope yang sesuai ditolak dengan `403 Forbidden`.
- Setiap request API key memperbarui `last_used_at`.

### API Artikel dan Media

- Client dapat membuat artikel memakai API key valid.
- Artikel via API menyimpan identitas admin pembuat API key.
- Client dapat upload foto memakai API key valid dan scope `media:upload`.
- Client dapat menyimpan URL foto memakai API key valid dan scope `media:url`.
- Public hanya melihat artikel yang statusnya `published`.

### Profile

- User login dapat membuka dan mengubah profil sendiri.
- User tidak dapat mengubah profil user lain.
- User dapat mengubah foto profil.
- Avatar hanya menerima tipe file gambar yang diizinkan.

### Writer Management

- Admin dapat membuat writer baru.
- Admin dapat menonaktifkan/menghapus writer.
- Non-admin tidak bisa mengakses manajemen writer.
- Writer yang dihapus/di-nonaktifkan tidak bisa login.
- Artikel lama writer tetap aman dan tidak orphan.

## Error State

- `DATABASE_URL` kosong atau DB down.
- API key tidak ada, salah, revoked, atau expired.
- API key tidak punya scope yang dibutuhkan.
- Body JSON invalid.
- Upload file terlalu besar atau bukan gambar.
- Slug artikel duplikat.
- Email writer duplikat.
- User mencoba mengubah profil user lain.
- Admin mencoba hard delete writer yang masih punya artikel.

## Security

- API key plaintext hanya muncul sekali.
- Simpan API key dalam bentuk hash, bukan plaintext.
- Gunakan constant-time compare saat validasi hash/key.
- Batasi ukuran upload dan tipe MIME.
- Validasi URL foto eksternal agar skema hanya `http`/`https`.
- Semua endpoint mutasi dashboard wajib login.
- Semua endpoint admin wajib cek role admin di server.
- Endpoint API wajib rate limit per API key.
- Log request API minimal: `api_key_id`, `admin_id`, `path`, `status`, tanpa mencatat secret key.

## Prioritas

P1 setelah PostgreSQL-only runtime stabil, karena membuka integrasi eksternal dan memperkuat admin CMS.

## Estimasi

- Database migration dan model API key/profile: 0.5–1 hari.
- Dashboard API key: 1 hari.
- API create article + media upload/url: 1–2 hari.
- Profile user + avatar: 1 hari.
- Admin manage writer: 1 hari.
- Security polish, validasi, dan dokumentasi API: 1 hari.

# PRD — Admin, Review, dan Role Artikel

## Ringkasan

Fitur ini menambahkan role dan workflow editorial agar artikel dari penulis dapat direview editor/admin sebelum publish. Ini membuat CMS lebih aman untuk tim multi-penulis.

## Tujuan

- Role user membatasi akses fitur.
- Penulis dapat submit artikel untuk review.
- Editor dapat approve atau request revision.
- Admin dapat mengelola semua artikel dan user.
- Artikel hanya publish setelah memenuhi workflow yang disepakati.

## Non-Goals MVP

- Komentar internal threaded.
- Revision diff lengkap.
- Notification email.
- Multi-step approval kompleks.
- Audit log detail semua field.

## Target User

- Writer/Penulis.
- Editor.
- Admin.

## Role & Permission

### Writer

- Create artikel.
- Edit artikel milik sendiri jika status `draft` atau `needs_revision`.
- Submit artikel untuk review.
- Tidak bisa publish langsung jika workflow review aktif.

### Editor

- Melihat artikel submitted.
- Edit artikel semua penulis.
- Approve/publish artikel.
- Request revision.

### Admin

- Semua akses editor.
- Manage user.
- Delete artikel.
- Override status artikel.

## Status Artikel

- `draft`
- `submitted`
- `needs_revision`
- `published`
- `archived`

## Scope MVP

### Route Dashboard

- `GET /dashboard/review` — daftar artikel submitted.
- `POST /dashboard/articles/{id}/submit` — submit review.
- `POST /dashboard/articles/{id}/approve` — approve/publish.
- `POST /dashboard/articles/{id}/request-revision` — minta revisi.
- `POST /dashboard/articles/{id}/archive` — archive artikel.

## Database Update

Tambahan field di `articles`:

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `review_status` | varchar | yes | Bisa pakai `status` utama |
| `review_note` | text | no | Catatan editor |
| `reviewed_by` | FK users.id | no | Editor/admin reviewer |
| `reviewed_at` | timestamp | no | Waktu review |

Optional table:

### `article_reviews`

| Field | Type | Required | Keterangan |
| --- | --- | --- | --- |
| `id` | integer/uuid | yes | Primary key |
| `article_id` | FK articles.id | yes | Artikel |
| `reviewer_id` | FK users.id | yes | Reviewer |
| `action` | varchar | yes | `approve`, `request_revision`, `archive` |
| `note` | text | no | Catatan |
| `created_at` | timestamp | yes | Waktu aksi |

## Acceptance Criteria

- Writer dapat submit artikel dari draft ke submitted.
- Editor melihat daftar artikel submitted.
- Editor dapat publish artikel submitted.
- Editor dapat mengirim artikel ke needs_revision dengan catatan.
- Writer dapat mengedit artikel needs_revision.
- Writer tidak dapat approve artikelnya sendiri kecuali role admin/editor.
- Public hanya melihat artikel `published`.

## Error State

- User tidak punya role.
- Artikel tidak ditemukan.
- Status artikel tidak valid untuk aksi.
- Catatan revisi kosong saat request revision.
- DB error.

## Security

- Role dicek di server, bukan hanya UI.
- Semua aksi mutasi wajib login.
- CSRF protection untuk form POST.
- Log aksi review minimal action + actor + article.

## Prioritas

P2 untuk MVP awal. Bisa masuk setelah login + CRUD artikel stabil.

## Estimasi

- Basic role permission: 0.5–1 hari.
- Review workflow: 1–2 hari.
- Audit log + polish: 1 hari tambahan.

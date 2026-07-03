# Flowchart — API Key Admin, Profile User, dan Manajemen Writer

## 1. Generate dan Revoke API Key

```mermaid
flowchart TD
    A[Admin login dashboard] --> B[Buka halaman API Keys]
    B --> C[Klik generate API key]
    C --> D[Isi nama, scope, dan expired opsional]
    D --> E{Role admin valid?}
    E -- Tidak --> F[Forbidden]
    E -- Ya --> G{Input valid?}
    G -- Tidak --> H[Tampilkan error validasi]
    G -- Ya --> I[Generate secret API key]
    I --> J[Hash secret key]
    J --> K[Simpan api_keys ke PostgreSQL]
    K --> L[Tampilkan plaintext key satu kali]
    L --> M[Admin simpan key di sistem eksternal]

    B --> N[Admin pilih revoke key]
    N --> O{Role admin valid?}
    O -- Tidak --> F
    O -- Ya --> P[Set status revoked dan revoked_at]
    P --> Q[API key tidak bisa dipakai lagi]
```

## 2. API Key untuk Post Artikel

```mermaid
flowchart TD
    A[Client kirim POST /api/v1/articles] --> B[Ambil Authorization Bearer token]
    B --> C{API key ada?}
    C -- Tidak --> D[401 Unauthorized]
    C -- Ya --> E[Hash/lookup API key di PostgreSQL]
    E --> F{Key aktif dan belum expired?}
    F -- Tidak --> D
    F -- Ya --> G{Scope articles:create ada?}
    G -- Tidak --> H[403 Forbidden]
    G -- Ya --> I[Parse dan validasi body artikel]
    I --> J{Payload valid?}
    J -- Tidak --> K[400 Bad Request]
    J -- Ya --> L[Set author_id ke admin pembuat key]
    L --> M[Set created_by_api_key_id]
    M --> N[Set api_actor_admin_id]
    N --> O[Simpan artikel ke PostgreSQL]
    O --> P[Update api_keys.last_used_at]
    P --> Q[Return 201 Created]
```

## 3. API Key untuk Upload Foto atau URL Foto

```mermaid
flowchart TD
    A[Client request media API] --> B{Endpoint?}

    B -- Upload file --> C[POST /api/v1/media/upload]
    C --> D[Validasi API key]
    D --> E{Scope media:upload ada?}
    E -- Tidak --> F[403 Forbidden]
    E -- Ya --> G[Validasi file gambar]
    G --> H{File valid?}
    H -- Tidak --> I[400 Bad Request]
    H -- Ya --> J[Simpan file ke UPLOAD_DIR]
    J --> K[Simpan metadata media source upload]

    B -- URL foto --> L[POST /api/v1/media/url]
    L --> M[Validasi API key]
    M --> N{Scope media:url ada?}
    N -- Tidak --> F
    N -- Ya --> O[Validasi URL http/https]
    O --> P{URL valid?}
    P -- Tidak --> I
    P -- Ya --> Q[Simpan metadata media source external_url]

    K --> R[Set owner admin pembuat key]
    Q --> R
    R --> S[Set created_by_api_key_id]
    S --> T[Update api_keys.last_used_at]
    T --> U[Return media URL]
```

## 4. User Mengatur Profile dan Foto Profil

```mermaid
flowchart TD
    A[User login dashboard] --> B[Buka /dashboard/profile]
    B --> C[Form tampil dengan data user]
    C --> D{Aksi user?}

    D -- Update data profil --> E[Edit nama, bio, phone]
    E --> F[Submit POST /dashboard/profile]
    F --> G{User mengubah dirinya sendiri?}
    G -- Tidak --> H[403 Forbidden]
    G -- Ya --> I{Input valid?}
    I -- Tidak --> J[Tampilkan error validasi]
    I -- Ya --> K[Update users di PostgreSQL]
    K --> L[Tampilkan profil terbaru]

    D -- Update avatar --> M[Pilih/upload foto profil]
    M --> N[POST /dashboard/profile/avatar]
    N --> O{File gambar valid?}
    O -- Tidak --> J
    O -- Ya --> P[Simpan file ke UPLOAD_DIR]
    P --> Q[Update users.avatar_url]
    Q --> L
```

## 5. Admin Menambahkan dan Menghapus Writer

```mermaid
flowchart TD
    A[Admin login dashboard] --> B[Buka /dashboard/users]
    B --> C{Aksi admin?}

    C -- Tambah writer --> D[Buka form writer baru]
    D --> E[Isi nama, email, password, status]
    E --> F{Role admin valid?}
    F -- Tidak --> G[403 Forbidden]
    F -- Ya --> H{Input valid dan email unik?}
    H -- Tidak --> I[Tampilkan error validasi]
    H -- Ya --> J[Hash password bcrypt]
    J --> K[Insert user role writer ke PostgreSQL]
    K --> L[Writer bisa login jika active]

    C -- Hapus/nonaktifkan writer --> M[Pilih writer]
    M --> N{Role admin valid?}
    N -- Tidak --> G
    N -- Ya --> O{Writer punya artikel/media?}
    O -- Ya --> P[Soft delete atau set status inactive]
    O -- Tidak --> Q[Hard delete boleh dilakukan]
    P --> R[Writer tidak bisa login]
    Q --> R
    R --> S[Daftar writer diperbarui]
```

## Flow Utama Integrasi Eksternal

```mermaid
flowchart TD
    A[Admin membuat API key] --> B[Admin memberikan key ke client eksternal]
    B --> C[Client membuat artikel atau upload media]
    C --> D[Server validasi API key dan scope]
    D --> E{Valid?}
    E -- Tidak --> F[Reject request]
    E -- Ya --> G[Simpan data ke PostgreSQL]
    G --> H[Catat admin pembuat key sebagai actor]
    H --> I[Data tampil di dashboard admin]
    I --> J{Artikel published?}
    J -- Tidak --> K[Tetap di dashboard sebagai draft/submitted]
    J -- Ya --> L[Tampil di halaman publik]
```

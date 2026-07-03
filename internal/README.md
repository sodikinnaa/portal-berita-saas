# Internal layering

Kode di `internal` dipisahkan mengikuti layered / clean architecture ringan:

- `cms`: domain layer. Berisi entity, konstanta domain, error domain, dan aturan bisnis murni yang tidak bergantung ke HTTP, database, atau file system.
- `application`: application layer. Berisi port/contract use case yang dipakai delivery layer dan diimplementasikan infrastructure layer.
- `httpserver`: delivery layer. Berisi routing, handler HTTP, middleware, parsing form, dan rendering response.
- `infrastructure`: infrastructure layer. Berisi adapter teknis seperti persistence PostgreSQL/JSON yang mengimplementasikan contract dari `application`.
- `config` dan `web`: supporting layer untuk konfigurasi, template, dan static asset.

Arah dependency yang diharapkan:

```text
httpserver -> application -> cms
infrastructure -> application + cms
config/web -> berdiri sendiri sesuai kebutuhan runtime
```

Aturan praktis:

1. Entity, error, validasi bisnis, slug/id helper domain masuk ke `internal/cms`.
2. Kontrak use case/repository yang dibutuhkan handler masuk ke `internal/application/<feature>`.
3. Implementasi database/file/API eksternal masuk ke `internal/infrastructure/...`.
4. Handler HTTP tidak langsung bergantung ke adapter database tertentu; gunakan contract dari `application`.
5. Adapter infrastructure boleh bergantung ke domain dan application contract, tetapi domain tidak boleh bergantung balik ke infrastructure.

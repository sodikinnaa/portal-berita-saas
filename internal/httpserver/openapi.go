package httpserver

const openAPISpecJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "NewsPaper CMS API",
    "version": "1.0.0",
    "description": "API untuk membuat artikel dan media dari integrasi eksternal. Gunakan API key dari Dashboard > API Keys sebagai Bearer token."
  },
  "servers": [
    {
      "url": "/",
      "description": "Current host"
    }
  ],
  "tags": [
    {
      "name": "Articles",
      "description": "Endpoint artikel"
    },
    {
      "name": "Media",
      "description": "Endpoint media"
    },
    {
      "name": "Health",
      "description": "Health check"
    }
  ],
  "paths": {
    "/api/v1/articles": {
      "get": {
        "tags": ["Articles"],
        "summary": "Dapatkan daftar artikel",
        "description": "Mengambil semua artikel menggunakan API key admin. Scope yang dibutuhkan: articles:read.",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Daftar artikel berhasil diambil",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/Article" }
                }
              }
            }
          },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      },
      "post": {
        "tags": ["Articles"],
        "summary": "Buat artikel dari API",
        "description": "Membuat artikel menggunakan API key admin. Scope yang dibutuhkan: articles:create.",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/ArticleInput" },
              "example": {
                "title": "Judul Artikel dari API",
                "slug": "judul-artikel-dari-api",
                "excerpt": "Ringkasan singkat artikel.",
                "content": "Isi artikel lengkap.",
                "category": "Teknologi",
                "hero_image_url": "https://example.com/image.jpg",
                "image_source": "Foto: Antara/Nama Fotografer",
                "status": "draft",
                "source_url": "https://example.com/sumber-asli"
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Artikel berhasil dibuat",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Article" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/v1/media/upload": {
      "post": {
        "tags": ["Media"],
        "summary": "Upload media gambar",
        "description": "Upload file media melalui multipart form. Scope yang dibutuhkan: media:upload.",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "multipart/form-data": {
              "schema": {
                "type": "object",
                "required": ["file"],
                "properties": {
                  "file": {
                    "type": "string",
                    "format": "binary",
                    "description": "File gambar, maksimal 5MB."
                  }
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Media berhasil diupload",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Media" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/v1/media/url": {
      "post": {
        "tags": ["Media"],
        "summary": "Daftarkan URL media eksternal",
        "description": "Mendaftarkan media dari URL eksternal tanpa upload file. Scope yang dibutuhkan: media:url.",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/ExternalMediaInput" },
              "example": {
                "url": "https://example.com/image.jpg",
                "original_name": "image.jpg"
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Media eksternal berhasil dibuat",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Media" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/healthz": {
      "get": {
        "tags": ["Health"],
        "summary": "Health check",
        "responses": {
          "200": {
            "description": "Service hidup",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Health" }
              }
            }
          }
        }
      }
    },
    "/readyz": {
      "get": {
        "tags": ["Health"],
        "summary": "Readiness check",
        "responses": {
          "200": {
            "description": "Service siap menerima request",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Health" }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "API key",
        "description": "Isi dengan API key dari dashboard. Contoh: Bearer portal..."
      }
    },
    "responses": {
      "BadRequest": {
        "description": "Request tidak valid",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" }
          }
        }
      },
      "Unauthorized": {
        "description": "API key tidak valid atau tidak dikirim",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" }
          }
        }
      },
      "Forbidden": {
        "description": "Scope atau role tidak cukup",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" }
          }
        }
      }
    },
    "schemas": {
      "ArticleInput": {
        "type": "object",
        "required": ["title", "content", "category"],
        "properties": {
          "title": { "type": "string" },
          "slug": { "type": "string", "description": "Opsional. Jika kosong, server membuat dari title." },
          "excerpt": { "type": "string" },
          "content": { "type": "string" },
          "category": { "type": "string" },
          "hero_image_url": { "type": "string" },
          "image_source": { "type": "string", "description": "Opsional. Kredit/sumber gambar utama." },
          "status": { "type": "string", "enum": ["draft", "submitted", "published"] },
          "source_url": { "type": "string", "description": "Opsional. Link rujukan/sumber jika artikel rewrite." }
        }
      },
      "ExternalMediaInput": {
        "type": "object",
        "required": ["url"],
        "properties": {
          "url": { "type": "string", "format": "uri" },
          "original_name": { "type": "string" }
        }
      },
      "Article": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "author_id": { "type": "string" },
          "title": { "type": "string" },
          "slug": { "type": "string" },
          "excerpt": { "type": "string" },
          "content": { "type": "string" },
          "category": { "type": "string" },
          "hero_image_url": { "type": "string" },
          "image_source": { "type": "string" },
          "status": { "type": "string" },
          "source_url": { "type": "string" },
          "created_at": { "type": "string", "format": "date-time" },
          "updated_at": { "type": "string", "format": "date-time" }
        }
      },
      "Media": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "owner_id": { "type": "string" },
          "filename": { "type": "string" },
          "original_name": { "type": "string" },
          "mime_type": { "type": "string" },
          "size_bytes": { "type": "integer", "format": "int64" },
          "url": { "type": "string" },
          "source": { "type": "string" },
          "created_at": { "type": "string", "format": "date-time" }
        }
      },
      "Error": {
        "type": "object",
        "properties": {
          "error": { "type": "string" }
        }
      },
      "Health": {
        "type": "object",
        "properties": {
          "status": { "type": "string" }
        }
      }
    }
  }
}`

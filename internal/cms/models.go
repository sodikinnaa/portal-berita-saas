package cms

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrValidation   = errors.New("validation error")
	ErrConflict     = errors.New("conflict")
)

const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleWriter = "writer"

	StatusActive   = "active"
	StatusInactive = "inactive"

	ArticleDraft         = "draft"
	ArticleSubmitted     = "submitted"
	ArticleNeedsRevision = "needs_revision"
	ArticlePublished     = "published"
	ArticleArchived      = "archived"
)

type User struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"password_hash"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	Bio          string     `json:"bio"`
	Phone        string     `json:"phone"`
	AvatarURL    string     `json:"avatar_url"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Article struct {
	ID                string     `json:"id"`
	AuthorID          string     `json:"author_id"`
	Title             string     `json:"title"`
	Slug              string     `json:"slug"`
	Excerpt           string     `json:"excerpt"`
	Content           string     `json:"content"`
	Category          string     `json:"category"`
	HeroImageURL      string     `json:"hero_image_url"`
	Status            string     `json:"status"`
	ReviewNote        string     `json:"review_note"`
	ReviewedBy        string     `json:"reviewed_by"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	PublishedAt       *time.Time `json:"published_at,omitempty"`
	CreatedByAPIKeyID string     `json:"created_by_api_key_id"`
	APIActorAdminID   string     `json:"api_actor_admin_id"`
	SourceURL         string     `json:"source_url"`
	ImageSource       string     `json:"image_source"`
	IsPremium         bool       `json:"is_premium"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Category struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	ShowInNavbar bool      `json:"show_in_navbar"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CategoryInput struct {
	Name         string
	Slug         string
	ShowInNavbar bool
}

type Media struct {
	ID                string    `json:"id"`
	OwnerID           string    `json:"owner_id"`
	Filename          string    `json:"filename"`
	OriginalName      string    `json:"original_name"`
	MIMEType          string    `json:"mime_type"`
	SizeBytes         int64     `json:"size_bytes"`
	URL               string    `json:"url"`
	Source            string    `json:"source"`
	CreatedByAPIKeyID string    `json:"created_by_api_key_id"`
	CreatedAt         time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type ArticleInput struct {
	Title        string `json:"title"`
	Slug         string `json:"slug"`
	Excerpt      string `json:"excerpt"`
	Content      string `json:"content"`
	Category     string `json:"category"`
	HeroImageURL string `json:"hero_image_url"`
	Status       string `json:"status"`
	SourceURL    string `json:"source_url"`
	ImageSource  string `json:"image_source"`
}

type APIKey struct {
	ID         string     `json:"id"`
	AdminID    string     `json:"admin_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Secret     string     `json:"secret,omitempty"`
	Scopes     []string   `json:"scopes"`
	Status     string     `json:"status"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type APIKeyWithSecret struct {
	APIKey
	Secret string `json:"secret"`
}

type APIKeyInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

type APIPrincipal struct {
	Key   APIKey `json:"key"`
	Admin User   `json:"admin"`
}

type ProfileInput struct {
	Name      string
	Bio       string
	Phone     string
	AvatarURL string
}

type WriterInput struct {
	Name     string
	Email    string
	Password string
	Status   string
}

type ExternalMediaInput struct {
	URL          string
	OriginalName string
}

const (
	APIKeyStatusActive  = "active"
	APIKeyStatusRevoked = "revoked"

	ScopeArticlesCreate = "articles:create"
	ScopeArticlesRead   = "articles:read"
	ScopeMediaUpload    = "media:upload"
	ScopeMediaURL       = "media:url"

	MediaSourceDashboardUpload = "dashboard_upload"
	MediaSourceAPIUpload       = "api_upload"
	MediaSourceExternalURL     = "external_url"
)

type APIKeyBackup struct {
	ID         string     `json:"id"`
	AdminID    string     `json:"admin_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	KeyHash    string     `json:"key_hash"`
	KeySecret  string     `json:"key_secret"`
	Scopes     []string   `json:"scopes"`
	Status     string     `json:"status"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type DatabaseBackup struct {
	Version      int               `json:"version"`
	Timestamp    time.Time         `json:"timestamp"`
	Users        []User            `json:"users"`
	Categories   []Category        `json:"categories"`
	Articles     []Article         `json:"articles"`
	APIKeys      []APIKeyBackup    `json:"api_keys"`
	SiteSettings map[string]string `json:"site_settings"`
	Blacklist    []string          `json:"blacklist"`
	Media        []Media           `json:"media"`
}

type Proxy struct {
	ID          string     `json:"id"`
	IP          string     `json:"ip"`
	Port        int        `json:"port"`
	Username    string     `json:"username"`
	Password    string     `json:"password"`
	Protocol    string     `json:"protocol"` // "http", "socks5"
	Status      string     `json:"status"`   // "active", "dead", "checking"
	LastChecked *time.Time `json:"last_checked,omitempty"`
	LatencyMS   *int       `json:"latency_ms,omitempty"`
	LastUsed      *time.Time `json:"last_used,omitempty"`
	BytesSent     int64      `json:"bytes_sent"`
	BytesReceived int64      `json:"bytes_received"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ProxyInput struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Protocol string `json:"protocol"`
}

func (p Proxy) FormattedBandwidth() string {
	total := p.BytesSent + p.BytesReceived
	const unit = 1024
	if total < unit {
		return fmt.Sprintf("%d B", total)
	}
	div, exp := int64(unit), 0
	for n := total / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(total)/float64(div), "KMGTPE"[exp])
}

type WebshareKey struct {
	ID        string    `json:"id"`
	APIKey    string    `json:"api_key"`
	Label     string    `json:"label"`
	BytesUsed int64     `json:"bytes_used"`
	CreatedAt time.Time `json:"created_at"`
}

func (wk WebshareKey) MaskedKey() string {
	if len(wk.APIKey) <= 8 {
		return "****"
	}
	return "****" + wk.APIKey[len(wk.APIKey)-6:]
}

func (wk WebshareKey) FormattedBandwidth() string {
	total := wk.BytesUsed
	const unit = 1024
	if total < unit {
		return fmt.Sprintf("%d B", total)
	}
	div, exp := int64(unit), 0
	for n := total / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(total)/float64(div), "KMGTPE"[exp])
}

func (wk WebshareKey) Percentage() float64 {
	const quotaBytes int64 = 1024 * 1024 * 1024
	pct := (float64(wk.BytesUsed) / float64(quotaBytes)) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

type ChartDataPoint struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

type AppUser struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"password_hash"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type AppUserInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type Email struct {
	ID           string     `json:"id"`
	UserID       *string    `json:"user_id,omitempty"`
	Direction    string     `json:"direction"` // 'inbound' or 'outbound'
	Sender       string     `json:"sender"`
	SenderName   string     `json:"sender_name"`
	Recipient    string     `json:"recipient"`
	Subject      string     `json:"subject"`
	BodyHTML     string     `json:"body_html"`
	BodyText     string     `json:"body_text"`
	Status       string     `json:"status"` // 'unread', 'read', 'sent', 'failed'
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	Metadata     string     `json:"metadata,omitempty"` // Raw metadata JSON string
}




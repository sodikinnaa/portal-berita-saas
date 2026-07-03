package cms

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func ValidateCategoryInput(input CategoryInput) (string, string, error) {
	name := strings.TrimSpace(input.Name)
	if len(name) < 3 {
		return "", "", fmt.Errorf("%w: category name must be at least 3 characters", ErrValidation)
	}
	slug := NormalizeSlug(input.Slug)
	if slug == "" {
		slug = Slugify(name)
	}
	if slug == "" {
		return "", "", fmt.Errorf("%w: category slug is required", ErrValidation)
	}
	return name, slug, nil
}

func ValidateArticleInput(input ArticleInput, strict bool) error {
	if len(strings.TrimSpace(input.Title)) < 5 {
		return fmt.Errorf("%w: title must be at least 5 characters", ErrValidation)
	}
	if strict && len(VisibleArticleText(input.Content)) < 1 {
		return fmt.Errorf("%w: content is required", ErrValidation)
	}
	if input.Status != "" {
		switch input.Status {
		case ArticleDraft, ArticleSubmitted, ArticleNeedsRevision, ArticlePublished, ArticleArchived:
		default:
			return fmt.Errorf("%w: invalid status", ErrValidation)
		}
	}
	return nil
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func VisibleArticleText(value string) string {
	value = strings.ReplaceAll(value, "&nbsp;", " ")
	value = html.UnescapeString(value)
	value = htmlTagPattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func ValidateArticleStatusForRole(user *User, status string) error {
	if status == "" || status == ArticleDraft || status == ArticleSubmitted {
		return nil
	}
	if user != nil && (user.Role == RoleAdmin || user.Role == RoleEditor) {
		return nil
	}
	return ErrForbidden
}

func CanEdit(user *User, article Article) bool {
	return user != nil && (user.Role == RoleAdmin || user.Role == RoleEditor || article.AuthorID == user.ID)
}

func CanDelete(user *User, article Article) bool {
	return user != nil && (user.Role == RoleAdmin || article.AuthorID == user.ID)
}

func CanManageCategories(user *User) bool {
	return user != nil && (user.Role == RoleAdmin || user.Role == RoleEditor)
}

func CanManageAPIKeys(user *User) bool {
	return user != nil && user.Role == RoleAdmin
}

func CanManageWriters(user *User) bool {
	return user != nil && user.Role == RoleAdmin
}

func ValidateProfileInput(input ProfileInput) (ProfileInput, error) {
	out := ProfileInput{Name: strings.TrimSpace(input.Name), Bio: strings.TrimSpace(input.Bio), Phone: strings.TrimSpace(input.Phone), AvatarURL: strings.TrimSpace(input.AvatarURL)}
	if len(out.Name) < 2 {
		return ProfileInput{}, fmt.Errorf("%w: name must be at least 2 characters", ErrValidation)
	}
	if len(out.Bio) > 500 {
		return ProfileInput{}, fmt.Errorf("%w: bio is too long", ErrValidation)
	}
	return out, nil
}

func ValidateWriterInput(input WriterInput, requirePassword bool) (WriterInput, error) {
	out := WriterInput{Name: strings.TrimSpace(input.Name), Email: strings.ToLower(strings.TrimSpace(input.Email)), Password: input.Password, Status: strings.TrimSpace(input.Status)}
	if len(out.Name) < 2 {
		return WriterInput{}, fmt.Errorf("%w: writer name must be at least 2 characters", ErrValidation)
	}
	if !strings.Contains(out.Email, "@") || !strings.Contains(out.Email, ".") {
		return WriterInput{}, fmt.Errorf("%w: valid email is required", ErrValidation)
	}
	if requirePassword && len(out.Password) < 6 {
		return WriterInput{}, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	if out.Status == "" {
		out.Status = StatusActive
	}
	if out.Status != StatusActive && out.Status != StatusInactive {
		return WriterInput{}, fmt.Errorf("%w: invalid user status", ErrValidation)
	}
	return out, nil
}

func ValidateAPIKeyInput(input APIKeyInput) (APIKeyInput, error) {
	out := APIKeyInput{Name: strings.TrimSpace(input.Name), ExpiresAt: input.ExpiresAt}
	if len(out.Name) < 3 {
		return APIKeyInput{}, fmt.Errorf("%w: api key name must be at least 3 characters", ErrValidation)
	}
	seen := map[string]bool{}
	for _, scope := range input.Scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		switch scope {
		case ScopeArticlesRead, ScopeArticlesCreate, ScopeMediaUpload, ScopeMediaURL:
			out.Scopes = append(out.Scopes, scope)
		default:
			return APIKeyInput{}, fmt.Errorf("%w: invalid api key scope", ErrValidation)
		}
		seen[scope] = true
	}
	if len(out.Scopes) == 0 {
		return APIKeyInput{}, fmt.Errorf("%w: api key scope is required", ErrValidation)
	}
	return out, nil
}

func HasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func ValidateExternalMediaURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: valid http/https image url is required", ErrValidation)
	}
	return rawURL, nil
}

func Slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return NormalizeSlug(b.String())
}

func NormalizeSlug(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), "-")
}

func RandomID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

func CanManageAppUsers(user *User) bool {
	return user != nil && (user.Role == RoleAdmin || user.Role == RoleEditor)
}

func ValidateAppUserInput(input AppUserInput, requirePassword bool) (AppUserInput, error) {
	out := AppUserInput{Name: strings.TrimSpace(input.Name), Email: strings.ToLower(strings.TrimSpace(input.Email)), Password: input.Password, Status: strings.TrimSpace(input.Status)}
	if len(out.Name) < 2 {
		return AppUserInput{}, fmt.Errorf("%w: app user name must be at least 2 characters", ErrValidation)
	}
	if !strings.Contains(out.Email, "@") || !strings.Contains(out.Email, ".") {
		return AppUserInput{}, fmt.Errorf("%w: valid email is required", ErrValidation)
	}
	if requirePassword && len(out.Password) < 6 {
		return AppUserInput{}, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	if out.Status == "" {
		out.Status = StatusActive
	}
	if out.Status != StatusActive && out.Status != StatusInactive {
		return AppUserInput{}, fmt.Errorf("%w: invalid app user status", ErrValidation)
	}
	return out, nil
}

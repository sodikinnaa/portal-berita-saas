package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chai2010/webp"
	"porta-berita/internal/cms"
)

type MigrationJob struct {
	ID         string    `json:"id"`
	TotalPosts int       `json:"total_posts"`
	Imported   int       `json:"imported"`
	Status     string    `json:"status"` // "idle", "running", "completed", "failed", "cancelled"
	ErrorMsg   string    `json:"error_msg,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	Cancelled  bool      `json:"cancelled"`
	Logs       []string  `json:"logs"`
}

type migrationViewData struct {
	User     *cms.User
	WpURL    string
	WpAPIKey string
	Success  string
	Error    string
	Job      MigrationJob
}

type wpPostPayload struct {
	Title        string `json:"title"`
	Slug         string `json:"slug"`
	Excerpt      string `json:"excerpt"`
	Content      string `json:"content"`
	Category     string `json:"category"`
	HeroImageURL string `json:"hero_image_url"`
	SourceURL    string `json:"source_url"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

var (
	categoryMigrationMu sync.Mutex
	articleMigrationMu  sync.Mutex
)

func (s *Server) dashboardMigration(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	s.migrationJobMu.RLock()
	jobCopy := s.migrationJob
	s.migrationJobMu.RUnlock()

	s.renderTemplate(w, "dashboard_migration.html", migrationViewData{
		User: user,
		Job:  jobCopy,
	})
}

func (s *Server) dashboardMigrationStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	s.migrationJobMu.RLock()
	jobCopy := s.migrationJob
	s.migrationJobMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobCopy)
}

func (s *Server) cancelWPMigration(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	s.migrationJobMu.Lock()
	if s.migrationJob.Status == "running" {
		s.migrationJob.Cancelled = true
		s.migrationJob.Status = "cancelled"
		timestamp := time.Now().Format("15:04:05")
		s.migrationJob.Logs = append(s.migrationJob.Logs, fmt.Sprintf("[%s] [WARNING] Pengguna membatalkan proses migrasi secara manual.", timestamp))
	}
	jobCopy := s.migrationJob
	s.migrationJobMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobCopy)
}

func (s *Server) addMigrationLog(line string) {
	s.migrationJobMu.Lock()
	defer s.migrationJobMu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	s.migrationJob.Logs = append(s.migrationJob.Logs, fmt.Sprintf("[%s] %s", timestamp, line))

	if len(s.migrationJob.Logs) > 1000 {
		s.migrationJob.Logs = s.migrationJob.Logs[len(s.migrationJob.Logs)-1000:]
	}
}

func (s *Server) runWPMigration(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	s.migrationJobMu.Lock()
	if s.migrationJob.Status == "running" {
		s.migrationJobMu.Unlock()
		s.renderTemplate(w, "dashboard_migration.html", migrationViewData{
			User:  user,
			Error: "Proses migrasi sedang berjalan.",
		})
		return
	}

	wpURL := strings.TrimRight(strings.TrimSpace(r.FormValue("wp_url")), "/")
	wpAPIKey := strings.TrimSpace(r.FormValue("wp_api_key"))

	if wpURL == "" || wpAPIKey == "" {
		s.migrationJobMu.Unlock()
		s.renderTemplate(w, "dashboard_migration.html", migrationViewData{
			User:     user,
			WpURL:    wpURL,
			WpAPIKey: wpAPIKey,
			Error:    "Semua field wajib diisi.",
		})
		return
	}

	// Initialize background Job
	s.migrationJob = MigrationJob{
		ID:         fmt.Sprintf("job_%d", time.Now().Unix()),
		TotalPosts: 0,
		Imported:   0,
		Status:     "running",
		StartedAt:  time.Now(),
		Cancelled:  false,
		Logs:       []string{fmt.Sprintf("[%s] [INFO] Menginisialisasi migrasi data dari %s...", time.Now().Format("15:04:05"), wpURL)},
	}
	s.migrationJobMu.Unlock()

	// Launch background migration job
	go s.performBackgroundMigration(user, wpURL, wpAPIKey)

	// Redirect back to dashboard page where progress will be monitored
	http.Redirect(w, r, "/dashboard/migration", http.StatusSeeOther)
}

func (s *Server) performBackgroundMigration(user *cms.User, wpURL, wpAPIKey string) {
	u, err := url.Parse(wpURL)
	var wpBaseURL string
	if err == nil {
		wpBaseURL = u.Scheme + "://" + u.Host
	} else {
		wpBaseURL = wpURL
	}

	// 1. Build in-memory cache of existing articles to avoid N+1 SQL queries during loop
	s.addMigrationLog("[INFO] Memuat cache indeks artikel dari database...")
	allArticles := s.store.ListArticles(user)

	existingSlugs := make(map[string]bool)
	existingSourceURLs := make(map[string]bool)
	var latestTime time.Time

	for _, art := range allArticles {
		existingSlugs[art.Slug] = true
		if art.SourceURL != "" {
			existingSourceURLs[art.SourceURL] = true
		}
		// Track latest imported article from WordPress to support incremental updates
		if art.ImageSource == "WordPress Migration" {
			if art.CreatedAt.After(latestTime) {
				latestTime = art.CreatedAt
			}
		}
	}
	s.addMigrationLog(fmt.Sprintf("[INFO] Cache indeks dimuat: %d artikel terdaftar.", len(allArticles)))

	var afterParam string
	if !latestTime.IsZero() {
		// Use UTC format to instruct WordPress WP_Query's date_query
		afterParam = latestTime.UTC().Format("2006-01-02T15:04:05")
		s.addMigrationLog(fmt.Sprintf("[INFO] Mengaktifkan sinkronisasi incremental untuk artikel baru setelah tanggal %s UTC.", afterParam))
	}

	s.addMigrationLog("[INFO] Menghubungi REST API WordPress untuk kalkulasi total data...")

	var apiBaseURL string
	if strings.Contains(wpURL, "rest_route=") || strings.Contains(wpURL, "/wp-json/") {
		apiBaseURL = wpURL
	} else {
		apiBaseURL = wpURL + "/wp-json/wp-to-portal/v1/posts"
	}

	// Append after parameter to page requests
	if afterParam != "" {
		if strings.Contains(apiBaseURL, "?") {
			apiBaseURL = apiBaseURL + "&after=" + url.QueryEscape(afterParam)
		} else {
			apiBaseURL = apiBaseURL + "?after=" + url.QueryEscape(afterParam)
		}
	}

	var apiURL string
	if strings.Contains(apiBaseURL, "?") {
		apiURL = fmt.Sprintf("%s&page=1&per_page=1&api_key=%s", apiBaseURL, wpAPIKey)
	} else {
		apiURL = fmt.Sprintf("%s?page=1&per_page=1&api_key=%s", apiBaseURL, wpAPIKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		s.failMigrationJob("Gagal menghubungi API WordPress: " + err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.failMigrationJob(fmt.Sprintf("WordPress API mengembalikan status %d", resp.StatusCode))
		return
	}

	totalPostsHeader := resp.Header.Get("X-WP-Total")
	totalPagesHeader := resp.Header.Get("X-WP-TotalPages")

	totalPosts, _ := strconv.Atoi(totalPostsHeader)
	totalPages, _ := strconv.Atoi(totalPagesHeader)

	if totalPosts == 0 {
		totalPosts = 0
		totalPages = 1
	}

	s.migrationJobMu.Lock()
	s.migrationJob.TotalPosts = totalPosts
	s.migrationJobMu.Unlock()

	s.addMigrationLog(fmt.Sprintf("[INFO] Koneksi sukses. Menemukan total %d artikel baru (%d halaman).", totalPosts, totalPages))

	if totalPosts == 0 {
		s.migrationJobMu.Lock()
		s.migrationJob.Status = "completed"
		s.migrationJobMu.Unlock()
		s.addMigrationLog("[SUCCESS] Semua artikel sudah sinkron. Tidak ada artikel baru untuk diimpor.")
		return
	}

	// Build categories map for fast lookup
	existingCategories := s.store.ListCategories()
	categoryMap := make(map[string]bool)
	for _, cat := range existingCategories {
		categoryMap[strings.ToLower(cat.Name)] = true
	}

	// 2. Loop through pages paginated (100 posts per page)
	perPage := 100
	if totalPages == 0 && totalPosts > 0 {
		totalPages = (totalPosts / perPage) + 1
	}

	for page := 1; page <= totalPages; page++ {
		s.migrationJobMu.RLock()
		cancelled := s.migrationJob.Cancelled
		s.migrationJobMu.RUnlock()
		if cancelled {
			return
		}

		s.addMigrationLog(fmt.Sprintf("[INFO] Mengunduh batch artikel halaman %d dari %d...", page, totalPages))

		var pageURL string
		if strings.Contains(apiBaseURL, "?") {
			pageURL = fmt.Sprintf("%s&page=%d&per_page=%d&api_key=%s", apiBaseURL, page, perPage, wpAPIKey)
		} else {
			pageURL = fmt.Sprintf("%s?page=%d&per_page=%d&api_key=%s", apiBaseURL, page, perPage, wpAPIKey)
		}

		pageResp, err := client.Get(pageURL)
		if err != nil {
			s.failMigrationJob("Gagal memuat halaman " + strconv.Itoa(page) + ": " + err.Error())
			return
		}

		if pageResp.StatusCode != http.StatusOK {
			pageResp.Body.Close()
			s.failMigrationJob(fmt.Sprintf("WordPress API halaman %d mengembalikan status %d", page, pageResp.StatusCode))
			return
		}

		var wpPosts []wpPostPayload
		decodeErr := json.NewDecoder(pageResp.Body).Decode(&wpPosts)
		pageResp.Body.Close()
		if decodeErr != nil {
			s.failMigrationJob("Gagal mengurai JSON di halaman " + strconv.Itoa(page) + ": " + decodeErr.Error())
			return
		}

		if len(wpPosts) == 0 {
			break
		}

		// Concurrently import posts in this page using a worker pool of 10 workers
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)

		for _, post := range wpPosts {
			s.migrationJobMu.RLock()
			cancelled := s.migrationJob.Cancelled
			s.migrationJobMu.RUnlock()
			if cancelled {
				return
			}

			wg.Add(1)
			go func(p wpPostPayload) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				s.migrationJobMu.RLock()
				cancelledWorker := s.migrationJob.Cancelled
				s.migrationJobMu.RUnlock()
				if cancelledWorker {
					return
				}

				err := s.importSinglePost(user, p, wpURL, wpBaseURL, categoryMap, existingSlugs, existingSourceURLs)
				if err == nil {
					s.migrationJobMu.Lock()
					s.migrationJob.Imported++
					s.migrationJobMu.Unlock()
				}
			}(post)
		}
		wg.Wait()
	}

	// 3. Mark completed
	s.migrationJobMu.Lock()
	if s.migrationJob.Status == "running" {
		s.migrationJob.Status = "completed"
		s.migrationJobMu.Unlock()
		s.addMigrationLog(fmt.Sprintf("[SUCCESS] Migrasi selesai sukses! Total %d artikel baru diimpor.", s.migrationJob.Imported))
	} else {
		s.migrationJobMu.Unlock()
	}
}

func (s *Server) importSinglePost(user *cms.User, post wpPostPayload, wpURL, wpBaseURL string, categoryMap map[string]bool, existingSlugs, existingSourceURLs map[string]bool) error {
	sourceURL := post.SourceURL
	if sourceURL == "" {
		sourceURL = wpBaseURL + "/?p=" + post.Slug
	}

	// 1. Deduplication checks (by slug & source URL) using in-memory cache maps
	articleMigrationMu.Lock()
	if existingSourceURLs[sourceURL] {
		s.addMigrationLog(fmt.Sprintf("[SKIP] Artikel '%s' dilewati (URL asli sudah terimpor).", post.Title))
		articleMigrationMu.Unlock()
		return fmt.Errorf("article with source url already exists")
	}

	if existingSlugs[post.Slug] {
		s.addMigrationLog(fmt.Sprintf("[SKIP] Artikel '%s' dilewati (slug '%s' sudah terpakai).", post.Title, post.Slug))
		articleMigrationMu.Unlock()
		return fmt.Errorf("article with slug already exists")
	}
	articleMigrationMu.Unlock()

	// 2. Category check & creation (Thread safe)
	catName := strings.TrimSpace(post.Category)
	if catName == "" {
		catName = "Uncategorized"
	}
	catNameLower := strings.ToLower(catName)

	categoryMigrationMu.Lock()
	if !categoryMap[catNameLower] {
		_, err := s.store.CreateCategory(user, cms.CategoryInput{
			Name:         catName,
			Slug:         strings.ReplaceAll(catNameLower, " ", "-"),
			ShowInNavbar: false,
		})
		if err == nil {
			categoryMap[catNameLower] = true
		}
	}
	categoryMigrationMu.Unlock()

	// 3. Download and replace Featured Image (Hero Image)
	localHeroImageURL := ""
	if post.HeroImageURL != "" {
		url, err := s.downloadAndSaveImage(user, post.HeroImageURL)
		if err == nil && url != "" {
			localHeroImageURL = url
		}
	}

	// 4. Download and replace Content Images
	content := post.Content
	imgRe := regexp.MustCompile(`<img[^>]+src="([^">]+)"`)
	matches := imgRe.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			oldURL := match[1]
			if strings.Contains(oldURL, "wp-content/uploads/") || strings.HasPrefix(oldURL, wpBaseURL) {
				localURL, err := s.downloadAndSaveImage(user, oldURL)
				if err == nil && localURL != "" {
					content = strings.ReplaceAll(content, oldURL, localURL)
				}
			}
		}
	}

	// Double check lock to make sure no race inserts during long download process
	articleMigrationMu.Lock()
	defer articleMigrationMu.Unlock()

	if existingSourceURLs[sourceURL] {
		s.addMigrationLog(fmt.Sprintf("[SKIP] Artikel '%s' dilewati (URL asli sudah terimpor).", post.Title))
		return fmt.Errorf("article with source url already exists")
	}

	if existingSlugs[post.Slug] {
		s.addMigrationLog(fmt.Sprintf("[SKIP] Artikel '%s' dilewati (slug '%s' sudah terpakai).", post.Title, post.Slug))
		return fmt.Errorf("article with slug already exists")
	}

	input := cms.ArticleInput{
		Title:        post.Title,
		Slug:         post.Slug,
		Excerpt:      post.Excerpt,
		Content:      content,
		Category:     catName,
		HeroImageURL: localHeroImageURL,
		Status:       cms.ArticlePublished,
		SourceURL:    sourceURL,
		ImageSource:  "WordPress Migration",
	}

	_, err := s.store.CreateArticle(user, input)
	if err == nil {
		existingSlugs[post.Slug] = true
		existingSourceURLs[sourceURL] = true
		s.addMigrationLog(fmt.Sprintf("[SUCCESS] Artikel '%s' berhasil diimpor (Kategori: %s).", post.Title, catName))
	} else {
		s.addMigrationLog(fmt.Sprintf("[ERROR] Gagal mengimpor artikel '%s': %v", post.Title, err))
	}
	return err
}

func (s *Server) failMigrationJob(errMsg string) {
	s.addMigrationLog("[ERROR] " + errMsg)
	s.migrationJobMu.Lock()
	s.migrationJob.Status = "failed"
	s.migrationJob.ErrorMsg = errMsg
	s.migrationJobMu.Unlock()
}

func (s *Server) downloadAndSaveImage(user *cms.User, imageURL string) (string, error) {
	if imageURL == "" {
		return "", nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch image: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return "", fmt.Errorf("URL is not an image: %s", mime)
	}

	if err := os.MkdirAll(s.cfg.UploadDir, 0o755); err != nil {
		return "", err
	}

	ext := ".webp"
	if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
		ext = ".jpg"
	} else if strings.Contains(mime, "png") {
		ext = ".png"
	} else if strings.Contains(mime, "gif") {
		ext = ".gif"
	}

	if lastSlash := strings.LastIndex(imageURL, "/"); lastSlash != -1 {
		filename := imageURL[lastSlash+1:]
		if dot := strings.LastIndex(filename, "."); dot != -1 {
			uExt := strings.ToLower(filename[dot:])
			if uExt == ".webp" || uExt == ".jpg" || uExt == ".jpeg" || uExt == ".png" || uExt == ".gif" {
				ext = uExt
			}
		}
	}

	var finalData []byte
	var finalExt string
	var finalMime string

	// Decode using registered image formats
	img, _, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		var buf bytes.Buffer
		err = webp.Encode(&buf, img, &webp.Options{Lossless: false, Quality: 80})
		if err == nil {
			finalData = buf.Bytes()
			finalExt = ".webp"
			finalMime = "image/webp"
		}
	}

	// Fallback to original format if decoding or encoding failed
	if finalData == nil {
		finalData = data
		finalExt = ext
		finalMime = mime
	}

	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), finalExt)
	destPath := filepath.Join(s.cfg.UploadDir, filename)

	if err := os.WriteFile(destPath, finalData, 0o644); err != nil {
		return "", err
	}

	originalName := "wp-imported"
	if lastSlash := strings.LastIndex(imageURL, "/"); lastSlash != -1 {
		originalName = imageURL[lastSlash+1:]
	}

	media, err := s.store.CreateMedia(user, filename, originalName, finalMime, "/uploads/"+filename, int64(len(finalData)))
	if err != nil {
		return "/uploads/" + filename, nil
	}

	return media.URL, nil
}

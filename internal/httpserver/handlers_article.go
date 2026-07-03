package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"porta-berita/internal/cms"
)

type dashboardArticlesViewData struct {
	User       *cms.User
	Articles   []articleListItem
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
	Pages      []int
	Draft      int
	Submitted  int
	Published  int
	Today      int
	FilterQuery string
}

type articleFormViewData struct {
	User       *cms.User
	Title      string
	Action     string
	Article    cms.Article
	Status     string
	Error      string
	CanPublish bool
	Categories []cms.Category
}

type articleWizardViewData struct {
	User         *cms.User
	Categories   []cms.Category
	DefaultModel string
	Error        string
}

type wizardFetchRequest struct {
	URL string `json:"url"`
}

type wizardFetchResponse struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	ImageURL string `json:"image_url,omitempty"`
	Error    string `json:"error,omitempty"`
}

type wizardRewriteRequest struct {
	Model             string `json:"model"`
	Title             string `json:"title"`
	Content           string `json:"content"`
	PromptInstruction string `json:"prompt_instruction"`
}

type wizardRewriteResponse struct {
	Title       string             `json:"title"`
	Excerpt     string             `json:"excerpt"`
	Content     string             `json:"content"`
	Error       string             `json:"error,omitempty"`
	ModelUsed   string             `json:"model_used,omitempty"`
	LatencyMs   int64              `json:"latency_ms,omitempty"`
	RawOutput   string             `json:"raw_output,omitempty"`
	TokenUsage  *wizardTokenUsage  `json:"token_usage,omitempty"`
	BackendLogs []string           `json:"backend_logs,omitempty"`
}

type wizardTokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type modelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) dashboardArticles(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	articles := s.store.ListArticles(user)
	
	filterNoThumb := r.URL.Query().Get("filter") == "no_thumbnail"
	if filterNoThumb {
		var filtered []cms.Article
		for _, a := range articles {
			invalid := false
			if a.HeroImageURL == "" {
				invalid = true
			} else if strings.HasPrefix(a.HeroImageURL, "/uploads/") {
				filePath := filepath.Join(s.cfg.UploadDir, strings.TrimPrefix(a.HeroImageURL, "/uploads/"))
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					invalid = true
				}
			}
			if invalid {
				filtered = append(filtered, a)
			}
		}
		articles = filtered
	}

	const pageSize = 10
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	total := len(articles)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * pageSize
	limit := offset + pageSize
	if limit > total {
		limit = total
	}

	var paginatedArticles []cms.Article
	if total > 0 && offset < total {
		paginatedArticles = articles[offset:limit]
	}

	pages := make([]int, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages = append(pages, i)
	}

	data := dashboardArticlesViewData{
		User:       user,
		Articles:   s.articleListItems(paginatedArticles),
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		PrevPage:   page - 1,
		NextPage:   page + 1,
		Pages:      pages,
		FilterQuery: r.URL.Query().Get("filter"),
	}
	data.Total, data.Draft, data.Submitted, data.Published, data.Today = articleStats(articles)
	s.renderTemplate(w, "dashboard_articles.html", data)
}

func (s *Server) newArticleForm(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderArticleForm(w, articleFormViewData{User: user, Title: "Tulis Artikel", Action: "/dashboard/articles", Article: cms.Article{Status: cms.ArticleDraft}, Status: cms.ArticleDraft})
}

func (s *Server) createArticle(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseArticleForm(r)
	if err != nil {
		s.renderArticleForm(w, articleFormViewData{User: user, Title: "Tulis Artikel", Action: "/dashboard/articles", Article: cms.Article{Status: cms.ArticleDraft}, Status: cms.ArticleDraft, Error: "Form tidak valid"})
		return
	}
	article, err := s.store.CreateArticle(user, input)
	if err != nil {
		formArticle := articleFromInput(input)
		s.renderArticleForm(w, articleFormViewData{User: user, Title: "Tulis Artikel", Action: "/dashboard/articles", Article: formArticle, Status: formStatus(formArticle), Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/articles/"+article.ID+"/edit", http.StatusFound)
}

func (s *Server) editArticleForm(w http.ResponseWriter, r *http.Request) {
	article, err := s.store.ArticleByID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderArticleForm(w, articleFormViewData{User: userFromRequest(r), Title: "Edit Artikel", Action: "/dashboard/articles/" + article.ID, Article: *article, Status: formStatus(*article)})
}

func (s *Server) updateArticle(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseArticleForm(r)
	if err != nil {
		article, _ := s.store.ArticleByID(r.PathValue("id"))
		formArticle := valueOrEmpty(article)
		s.renderArticleForm(w, articleFormViewData{User: user, Title: "Edit Artikel", Action: "/dashboard/articles/" + r.PathValue("id"), Article: formArticle, Status: formStatus(formArticle), Error: "Form tidak valid"})
		return
	}
	_, err = s.store.UpdateArticle(user, r.PathValue("id"), input)
	if err != nil {
		formArticle := articleFromInput(input)
		formArticle.ID = r.PathValue("id")
		s.renderArticleForm(w, articleFormViewData{User: user, Title: "Edit Artikel", Action: "/dashboard/articles/" + r.PathValue("id"), Article: formArticle, Status: formStatus(formArticle), Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/articles", http.StatusFound)
}

func (s *Server) deleteArticle(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteArticle(userFromRequest(r), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/articles", http.StatusFound)
}

type deleteBulkRequest struct {
	IDs []string `json:"ids"`
}

type deleteBulkResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) bulkBingThumbnail(w http.ResponseWriter, r *http.Request) {
	var req deleteBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, deleteBulkResponse{Success: false, Error: "JSON tidak valid"})
		return
	}

	user := userFromRequest(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, deleteBulkResponse{Success: false, Error: "Unauthorized"})
		return
	}

	var updatedCount int
	var errorsList []string

	for _, id := range req.IDs {
		article, err := s.store.ArticleByID(id)
		if err != nil {
			continue
		}

		query := strings.TrimSpace(article.Title)
		if query == "" {
			continue
		}

		// Format: /Title_With_Underscores
		formattedQuery := "/" + strings.ReplaceAll(query, " ", "_")
		bingUrl := "https://tse1.mm.bing.net/th?q=" + url.QueryEscape(formattedQuery)
		
		input := cms.ArticleInput{
			Title:        article.Title,
			Slug:         article.Slug,
			Excerpt:      article.Excerpt,
			Content:      article.Content,
			Category:     article.Category,
			HeroImageURL: bingUrl,
			Status:       article.Status,
			SourceURL:    article.SourceURL,
			ImageSource:  article.ImageSource,
		}

		_, err = s.store.UpdateArticle(user, id, input)
		if err != nil {
			s.log.Error("Failed to auto bing thumbnail in bulk", "id", id, "error", err)
			errorsList = append(errorsList, fmt.Sprintf("Artikel %s: %s", id, err.Error()))
		} else {
			updatedCount++
		}
	}

	if len(errorsList) > 0 && updatedCount == 0 {
		writeJSON(w, http.StatusInternalServerError, deleteBulkResponse{
			Success: false,
			Error:   strings.Join(errorsList, "; "),
		})
		return
	}

	writeJSON(w, http.StatusOK, deleteBulkResponse{
		Success: true,
	})
}

func (s *Server) bulkBingThumbnailAllMissing(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, deleteBulkResponse{Success: false, Error: "Method not allowed"})
		return
	}

	user := userFromRequest(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, deleteBulkResponse{Success: false, Error: "Unauthorized"})
		return
	}

	articles := s.store.ListArticles(user)
	var updatedCount int
	var errorsList []string

	for _, a := range articles {
		invalid := false
		if a.HeroImageURL == "" {
			invalid = true
		} else if strings.HasPrefix(a.HeroImageURL, "/uploads/") {
			filePath := filepath.Join(s.cfg.UploadDir, strings.TrimPrefix(a.HeroImageURL, "/uploads/"))
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				invalid = true
			}
		}

		if invalid {
			query := strings.TrimSpace(a.Title)
			if query == "" {
				continue
			}

			// Format: /Title_With_Underscores
			formattedQuery := "/" + strings.ReplaceAll(query, " ", "_")
			bingUrl := "https://tse1.mm.bing.net/th?q=" + url.QueryEscape(formattedQuery)
			
			input := cms.ArticleInput{
				Title:        a.Title,
				Slug:         a.Slug,
				Excerpt:      a.Excerpt,
				Content:      a.Content,
				Category:     a.Category,
				HeroImageURL: bingUrl,
				Status:       a.Status,
				SourceURL:    a.SourceURL,
				ImageSource:  a.ImageSource,
			}

			_, err := s.store.UpdateArticle(user, a.ID, input)
			if err != nil {
				s.log.Error("Failed to auto bing thumbnail for all missing", "id", a.ID, "error", err)
				errorsList = append(errorsList, fmt.Sprintf("Artikel %s: %s", a.ID, err.Error()))
			} else {
				updatedCount++
			}
		}
	}

	writeJSON(w, http.StatusOK, deleteBulkResponse{
		Success: true,
		Error:   fmt.Sprintf("Berhasil memproses %d artikel.", updatedCount),
	})
}

func (s *Server) deleteArticlesBulk(w http.ResponseWriter, r *http.Request) {
	var req deleteBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, deleteBulkResponse{Success: false, Error: "JSON tidak valid"})
		return
	}

	user := userFromRequest(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, deleteBulkResponse{Success: false, Error: "Unauthorized"})
		return
	}

	var deletedCount int
	var errorsList []string

	for _, id := range req.IDs {
		err := s.store.DeleteArticle(user, id)
		if err != nil {
			s.log.Error("Failed to delete article in bulk", "id", id, "error", err)
			errorsList = append(errorsList, fmt.Sprintf("Artikel %s: %s", id, err.Error()))
		} else {
			deletedCount++
		}
	}

	if len(errorsList) > 0 && deletedCount == 0 {
		writeJSON(w, http.StatusInternalServerError, deleteBulkResponse{
			Success: false,
			Error:   strings.Join(errorsList, "; "),
		})
		return
	}

	writeJSON(w, http.StatusOK, deleteBulkResponse{
		Success: true,
	})
}

func (s *Server) submitArticle(w http.ResponseWriter, r *http.Request) {
	s.transitionAndRedirect(w, r, s.store.SubmitArticle)
}

func (s *Server) approveArticle(w http.ResponseWriter, r *http.Request) {
	s.transitionAndRedirect(w, r, s.store.ApproveArticle)
}

func (s *Server) archiveArticle(w http.ResponseWriter, r *http.Request) {
	s.transitionAndRedirect(w, r, s.store.ArchiveArticle)
}

func (s *Server) requestRevision(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, err := s.store.RequestRevision(userFromRequest(r), r.PathValue("id"), r.FormValue("note"))
	if err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/articles", http.StatusFound)
}

func (s *Server) transitionAndRedirect(w http.ResponseWriter, r *http.Request, fn func(*cms.User, string) (*cms.Article, error)) {
	if _, err := fn(userFromRequest(r), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/articles", http.StatusFound)
}

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "file terlalu besar atau form invalid", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file wajib diisi", http.StatusBadRequest)
		return
	}
	defer file.Close()
	media, err := s.saveUpload(user, file, header)
	if err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	writeJSON(w, http.StatusCreated, media)
}

func (s *Server) saveUpload(user *cms.User, file multipart.File, header *multipart.FileHeader) (cms.Media, error) {
	peek := make([]byte, 512)
	n, _ := file.Read(peek)
	mime := http.DetectContentType(peek[:n])
	if !strings.HasPrefix(mime, "image/") {
		return cms.Media{}, fmt.Errorf("%w: hanya file gambar yang diizinkan", cms.ErrValidation)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return cms.Media{}, err
	}
	if err := os.MkdirAll(s.cfg.UploadDir, 0o755); err != nil {
		return cms.Media{}, err
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".bin"
	}
	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	destPath := filepath.Join(s.cfg.UploadDir, filename)
	dest, err := os.Create(destPath)
	if err != nil {
		return cms.Media{}, err
	}
	defer dest.Close()
	written, err := io.Copy(dest, io.LimitReader(file, 5<<20))
	if err != nil {
		return cms.Media{}, err
	}
	return s.store.CreateMedia(user, filename, header.Filename, mime, "/uploads/"+filename, written)
}

func (s *Server) saveUploadForAPI(principal cms.APIPrincipal, file multipart.File, header *multipart.FileHeader) (cms.Media, error) {
	peek := make([]byte, 512)
	n, _ := file.Read(peek)
	mime := http.DetectContentType(peek[:n])
	if !strings.HasPrefix(mime, "image/") {
		return cms.Media{}, fmt.Errorf("%w: hanya file gambar yang diizinkan", cms.ErrValidation)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return cms.Media{}, err
	}
	if err := os.MkdirAll(s.cfg.UploadDir, 0o755); err != nil {
		return cms.Media{}, err
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".bin"
	}
	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	destPath := filepath.Join(s.cfg.UploadDir, filename)
	dest, err := os.Create(destPath)
	if err != nil {
		return cms.Media{}, err
	}
	defer dest.Close()
	written, err := io.Copy(dest, io.LimitReader(file, 5<<20))
	if err != nil {
		return cms.Media{}, err
	}
	return s.store.CreateMediaFromAPI(principal, filename, header.Filename, mime, "/uploads/"+filename, cms.MediaSourceAPIUpload, written)
}

func (s *Server) dashboardArticleWizard(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	categories := s.store.ListCategories()
	settings := s.store.GetSettings()
	defaultModel := settings["ai_default_model"]
	if defaultModel == "" {
		defaultModel = "gemini-2.5-flash"
	}
	s.renderTemplate(w, "article_wizard.html", articleWizardViewData{
		User:         user,
		Categories:   categories,
		DefaultModel: defaultModel,
	})
}

func (s *Server) wizardFetch(w http.ResponseWriter, r *http.Request) {
	var req wizardFetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, wizardFetchResponse{Error: "Request JSON tidak valid"})
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, wizardFetchResponse{Error: "URL tidak boleh kosong"})
		return
	}

	parsedURL, err := url.Parse(req.URL)
	var host string
	if err == nil {
		host = parsedURL.Hostname()
	}
	if host != "" {
		isBlacklisted, _ := s.store.IsDomainBlacklisted(host)
		if isBlacklisted {
			writeJSON(w, http.StatusBadRequest, wizardFetchResponse{
				Error: "Halaman rujukan tidak dapat di-scrape karena domain website ini (" + host + ") terdaftar dalam blacklist.",
			})
			return
		}
	}

	client := &http.Client{
		Timeout: 6 * time.Second,
	}

	httpReq, err := http.NewRequestWithContext(r.Context(), "GET", req.URL, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, wizardFetchResponse{Error: err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, wizardFetchResponse{Error: "Halaman rujukan tidak dapat di-scrape. Detail error: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadRequest, wizardFetchResponse{Error: fmt.Sprintf("Website rujukan mengembalikan status HTTP %d.", resp.StatusCode)})
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, wizardFetchResponse{Error: "Gagal membaca data dari rujukan: " + err.Error()})
		return
	}

	htmlContent := string(bodyBytes)
	title, content := extractTitleAndContent(htmlContent)
	imageURL := extractThumbnail(htmlContent)

	// Validasi hasil scrape artikel
	cleanContent := strings.TrimSpace(content)
	cleanTitle := strings.TrimSpace(title)

	isFallbackContent := cleanContent == "Konten utama tidak berhasil diekstrak otomatis. Anda bisa menulis atau menyalinnya secara manual."
	if cleanTitle == "" || cleanTitle == "Judul tidak ditemukan" || cleanContent == "" || isFallbackContent {
		s.blacklistURLHost(req.URL)
		writeJSON(w, http.StatusBadRequest, wizardFetchResponse{
			Error: "Halaman rujukan tidak dapat di-scrape atau tidak memiliki konten berita yang valid. Domain website ini telah masuk ke blacklist.",
		})
		return
	}

	writeJSON(w, http.StatusOK, wizardFetchResponse{
		Title:    title,
		Content:  content,
		ImageURL: imageURL,
	})
}

func extractThumbnail(htmlContent string) string {
	// 1. Cek og:image
	ogImageRegs := []*regexp.Regexp{
		regexp.MustCompile(`(?i)<meta\s+[^>]*property=["']og:image["']\s+content=["'](.*?)["']`),
		regexp.MustCompile(`(?i)<meta\s+[^>]*content=["'](.*?)["']\s+property=["']og:image["']`),
	}
	for _, reg := range ogImageRegs {
		if matches := reg.FindStringSubmatch(htmlContent); len(matches) > 1 {
			return html.UnescapeString(strings.TrimSpace(matches[1]))
		}
	}

	// 2. Cek twitter:image
	twitterImageRegs := []*regexp.Regexp{
		regexp.MustCompile(`(?i)<meta\s+[^>]*name=["']twitter:image["']\s+content=["'](.*?)["']`),
		regexp.MustCompile(`(?i)<meta\s+[^>]*content=["'](.*?)["']\s+name=["']twitter:image["']`),
	}
	for _, reg := range twitterImageRegs {
		if matches := reg.FindStringSubmatch(htmlContent); len(matches) > 1 {
			return html.UnescapeString(strings.TrimSpace(matches[1]))
		}
	}

	// 3. Cek itemprop="image"
	itempropImageReg := regexp.MustCompile(`(?i)<meta\s+[^>]*itemprop=["']image["']\s+content=["'](.*?)["']`)
	if matches := itempropImageReg.FindStringSubmatch(htmlContent); len(matches) > 1 {
		return html.UnescapeString(strings.TrimSpace(matches[1]))
	}

	return ""
}

func extractTitleAndContent(htmlContent string) (string, string) {
	title := "Judul tidak ditemukan"
	ogTitleReg := regexp.MustCompile(`(?i)<meta\s+[^>]*property=["']og:title["']\s+content=["'](.*?)["']`)
	if matches := ogTitleReg.FindStringSubmatch(htmlContent); len(matches) > 1 {
		title = matches[1]
	} else {
		titleReg := regexp.MustCompile(`(?i)<title>(.*?)</title>`)
		if matches := titleReg.FindStringSubmatch(htmlContent); len(matches) > 1 {
			title = matches[1]
		}
	}
	title = html.UnescapeString(strings.TrimSpace(title))

	htmlContent = regexp.MustCompile(`(?is)<script.*?>.*?</script>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<style.*?>.*?</style>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<!--.*?-->`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<header.*?>.*?</header>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<footer.*?>.*?</footer>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<nav.*?>.*?</nav>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<aside.*?>.*?</aside>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<form.*?>.*?</form>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<ul.*?>.*?</ul>`).ReplaceAllString(htmlContent, "")


	pReg := regexp.MustCompile(`(?is)<p.*?>(.*?)</p>`)
	pMatches := pReg.FindAllStringSubmatch(htmlContent, -1)

	var paragraphs []string
	for _, match := range pMatches {
		pText := match[1]
		pText = regexp.MustCompile(`<.*?>`).ReplaceAllString(pText, "")
		pText = strings.TrimSpace(pText)
		pText = html.UnescapeString(pText)
		
		lowerText := strings.ToLower(pText)
		if strings.Contains(lowerText, "baca juga") || 
		   strings.Contains(lowerText, "loading...") || 
		   strings.Contains(lowerText, "promoted") || 
		   strings.Contains(lowerText, "yang sedang ramai dicari") ||
		   strings.Contains(lowerText, "terakhir yang dicari") {
			continue
		}

		if len(pText) > 40 {
			paragraphs = append(paragraphs, pText)
		}
	}

	content := strings.Join(paragraphs, "\n\n")
	if len(content) == 0 {
		content = "Konten utama tidak berhasil diekstrak otomatis. Anda bisa menulis atau menyalinnya secara manual."
	}
	return title, content
}

func (s *Server) wizardRewrite(w http.ResponseWriter, r *http.Request) {
	var req wizardRewriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, wizardRewriteResponse{Error: "Request JSON tidak valid"})
		return
	}

	startTime := time.Now()
	var backendLogs []string
	backendLogs = append(backendLogs, fmt.Sprintf("Menerima permintaan rewrite untuk model: %s", req.Model))

	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}

	endpointURL := strings.TrimSpace(settings["ai_endpoint_url"])

	if apiKey == "" || req.Model == "mock-rewrite" {
		backendLogs = append(backendLogs, "Menggunakan Mock AI Offline Demo (Tidak ada API Key atau mode mock dipilih)")
		if strings.TrimSpace(req.PromptInstruction) != "" {
			backendLogs = append(backendLogs, fmt.Sprintf("Membaca instruksi kustom: \"%s\"", req.PromptInstruction))
		}
		mockTitle := fmt.Sprintf("[REWRITE - MOCK] %s", req.Title)
		mockExcerpt := "Hasil rewrite otomatis berita menggunakan Mock AI untuk demonstrasi lokal."
		if len(req.Content) > 200 {
			mockExcerpt = req.Content[:190] + "..."
		}
		mockContent := fmt.Sprintf("<p>Berikut ini adalah versi penulisan ulang berita lokal: <strong>%s</strong>.</p>\n\n<p>Ditulis ulang dalam format mandiri berdasarkan rujukan asli:</p>\n\n<blockquote>%s</blockquote>", req.Title, req.Content)
		if strings.TrimSpace(req.PromptInstruction) != "" {
			mockContent += fmt.Sprintf("\n\n<p><em>(Catatan AI: Penulisan disesuaikan dengan instruksi: \"%s\")</em></p>", req.PromptInstruction)
		}
		
		// Simulasi latensi sedikit agar nampak proses
		time.Sleep(800 * time.Millisecond)

		backendLogs = append(backendLogs, 
			"Mendapatkan judul asli: "+req.Title,
			"Melakukan pembacaan teks berita...",
			"Menghasilkan output terstruktur...",
			"Proses rewrite mock selesai dengan sukses!",
		)

		writeJSON(w, http.StatusOK, wizardRewriteResponse{
			Title:       mockTitle,
			Excerpt:     mockExcerpt,
			Content:     mockContent,
			ModelUsed:   "mock-rewrite",
			LatencyMs:   time.Since(startTime).Milliseconds(),
			RawOutput:   fmt.Sprintf(`{"title": "%s", "excerpt": "%s", "content": "%s"}`, mockTitle, mockExcerpt, mockContent),
			TokenUsage: &wizardTokenUsage{
				PromptTokens:     len(req.Content) / 4,
				CompletionTokens: len(mockContent) / 4,
				TotalTokens:      (len(req.Content) + len(mockContent)) / 4,
			},
			BackendLogs: backendLogs,
		})
		return
	}

	isGemini := endpointURL == "" || strings.Contains(endpointURL, "googleapis.com")
	var apiURL string
	var reqBytes []byte
	var err error

	promptBuilder := strings.Builder{}
	promptBuilder.WriteString(`Anda adalah seorang jurnalis profesional senior untuk portal berita lokal. Tugas Anda adalah melakukan rekonstruksi dan penulisan ulang (rewrite) mendalam terhadap artikel berita rujukan dengan standar jurnalisme tinggi, bukan sekadar menyalin atau menerjemahkan kata demi kata.

Berikut adalah prinsip penulisan yang wajib Anda ikuti:
1. Narasi & Sudut Pandang Orisinal: Sajikan informasi utama dari berita rujukan menggunakan struktur kalimat baru yang dinamis, kaya kosakata, dan mengalir secara alami (tidak kaku).
2. Gaya Jurnalistik Kredibel: Tulis artikel dengan nada objektif, kredibel, dan mudah dipahami oleh masyarakat lokal, menerapkan kaidah 5W+1H secara proporsional.
3. Judul Kuat & Memikat: Buat judul baru yang menarik perhatian pembaca lokal, formal, informatif, dan mutlak bebas dari unsur clickbait.
4. Excerpt/Rangkuman Ringkas: Sediakan ringkasan berita dalam 1-2 kalimat padat (maksimal 220 karakter) sebagai pemandu pembaca.
5. Struktur Paragraf & Format HTML: Bagi artikel ke dalam beberapa paragraf logis yang teratur (pisahkan dengan baris baru ganda). Setiap paragraf wajib dibungkus dengan tag HTML <p>. Hindari pengulangan kalimat.`)

	if strings.TrimSpace(req.PromptInstruction) != "" {
		promptBuilder.WriteString(fmt.Sprintf("\n4. %s", strings.TrimSpace(req.PromptInstruction)))
		backendLogs = append(backendLogs, fmt.Sprintf("Menambahkan instruksi kustom ke prompt user: %s", req.PromptInstruction))
	}

	promptBuilder.WriteString(fmt.Sprintf(`

Artikel Asli:
Judul: %s
Konten: %s`, req.Title, req.Content))

	prompt := promptBuilder.String()

	if isGemini {
		if endpointURL == "" {
			endpointURL = "https://generativelanguage.googleapis.com/v1beta/models/"
		}
		if !strings.HasSuffix(endpointURL, "/") {
			endpointURL += "/"
		}
		apiURL = fmt.Sprintf("%s%s:generateContent?key=%s", endpointURL, req.Model, apiKey)
		backendLogs = append(backendLogs, fmt.Sprintf("Menyiapkan payload request Gemini (model: %s)...", req.Model))

		type geminiPart struct {
			Text string `json:"text"`
		}
		type geminiContent struct {
			Parts []geminiPart `json:"parts"`
		}
		type geminiSchemaProperty struct {
			Type string `json:"type"`
		}
		type geminiSchema struct {
			Type       string                          `json:"type"`
			Properties map[string]geminiSchemaProperty `json:"properties"`
			Required   []string                        `json:"required"`
		}
		type geminiGenConfig struct {
			ResponseMimeType string        `json:"responseMimeType"`
			ResponseSchema   *geminiSchema `json:"responseSchema,omitempty"`
		}
		type geminiReq struct {
			Contents         []geminiContent  `json:"contents"`
			GenerationConfig *geminiGenConfig `json:"generationConfig,omitempty"`
		}

		gReq := geminiReq{
			Contents: []geminiContent{
				{
					Parts: []geminiPart{
						{Text: prompt},
					},
				},
			},
			GenerationConfig: &geminiGenConfig{
				ResponseMimeType: "application/json",
				ResponseSchema: &geminiSchema{
					Type: "OBJECT",
					Properties: map[string]geminiSchemaProperty{
						"title":   {Type: "STRING"},
						"excerpt": {Type: "STRING"},
						"content": {Type: "STRING"},
					},
					Required: []string{"title", "excerpt", "content"},
				},
			},
		}
		reqBytes, err = json.Marshal(gReq)
	} else {
		apiURL = endpointURL
		if !strings.HasSuffix(apiURL, "/chat/completions") {
			if strings.HasSuffix(apiURL, "/") {
				apiURL += "chat/completions"
			} else {
				apiURL += "/chat/completions"
			}
		}
		backendLogs = append(backendLogs, fmt.Sprintf("Menyiapkan payload request OpenAI Compatible (model: %s)...", req.Model))

		type openAIMessage struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		type openAIResponseFormat struct {
			Type string `json:"type"`
		}
		type openAIReq struct {
			Model          string                `json:"model"`
			Messages       []openAIMessage       `json:"messages"`
			ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
		}

		oReq := openAIReq{
			Model: req.Model,
			Messages: []openAIMessage{
				{
					Role:    "system",
					Content: "Anda adalah asisten AI jurnalis profesional yang ahli menulis ulang artikel berita ke bahasa Indonesia. Anda wajib merespon hanya dengan format JSON yang valid.",
				},
				{
					Role:    "user",
					Content: prompt + "\n\nRespon WAJIB berupa objek JSON dengan key: title, excerpt, content.",
				},
			},
			ResponseFormat: &openAIResponseFormat{Type: "json_object"},
		}
		reqBytes, err = json.Marshal(oReq)
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
			Error:       "Gagal menyiapkan request payload: " + err.Error(),
			BackendLogs: backendLogs,
		})
		return
	}

	backendLogs = append(backendLogs, fmt.Sprintf("Mengirim HTTP POST ke endpoint: %s", apiURL))
	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", apiURL, strings.NewReader(string(reqBytes)))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
			Error:       err.Error(),
			BackendLogs: backendLogs,
		})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if !isGemini {
		// Mask API Key di logs demi keamanan
		maskedKey := "Bearer ***"
		if len(apiKey) > 8 {
			maskedKey = "Bearer " + apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
		}
		backendLogs = append(backendLogs, fmt.Sprintf("Menyetel header Authorization: %s", maskedKey))
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	var client *http.Client
	if settings["ai_enable_proxy"] == "true" {
		backendLogs = append(backendLogs, "Menggunakan proxy rotator untuk koneksi AI API...")
		client = s.getProxyHTTPClientWithTimeout(120 * time.Second)
	} else {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	backendLogs = append(backendLogs, "Menunggu respon dari AI API (Timeout: 120 detik)...")
	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
			Error:       "Gagal menghubungi AI API: " + err.Error(),
			BackendLogs: backendLogs,
		})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	backendLogs = append(backendLogs, fmt.Sprintf("Menerima HTTP status %d dari AI API", resp.StatusCode))
	
	bodyStr := string(bodyBytes)
	previewLen := 300
	if len(bodyStr) < previewLen {
		previewLen = len(bodyStr)
	}
	backendLogs = append(backendLogs, fmt.Sprintf("Preview respon mentah dari API (panjang %d karakter): %s", len(bodyStr), bodyStr[:previewLen]))

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadRequest, wizardRewriteResponse{
			Error:       fmt.Sprintf("AI API mengembalikan status HTTP %d: %s", resp.StatusCode, bodyStr),
			BackendLogs: backendLogs,
		})
		return
	}

	var rawJSONText string
	var tokenUsage *wizardTokenUsage

	if isGemini {
		type geminiCandidatePart struct {
			Text string `json:"text"`
		}
		type geminiCandidateContent struct {
			Parts []geminiCandidatePart `json:"parts"`
		}
		type geminiCandidate struct {
			Content geminiCandidateContent `json:"content"`
		}
		type geminiUsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		}
		type geminiResp struct {
			Candidates    []geminiCandidate    `json:"candidates"`
			UsageMetadata geminiUsageMetadata `json:"usageMetadata"`
		}

		var gResp geminiResp
		decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
		if err := decoder.Decode(&gResp); err != nil {
			writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
				Error:       "Gagal membaca struktur response Gemini: " + err.Error(),
				BackendLogs: backendLogs,
			})
			return
		}
		if len(gResp.Candidates) == 0 || len(gResp.Candidates[0].Content.Parts) == 0 {
			writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
				Error:       "Response Gemini kosong atau tidak valid",
				BackendLogs: backendLogs,
			})
			return
		}
		rawJSONText = gResp.Candidates[0].Content.Parts[0].Text
		tokenUsage = &wizardTokenUsage{
			PromptTokens:     gResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: gResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      gResp.UsageMetadata.TotalTokenCount,
		}
		backendLogs = append(backendLogs, fmt.Sprintf("Mengekstrak konten dari kandidat pertama Gemini (Token Input: %d, Output: %d)", tokenUsage.PromptTokens, tokenUsage.CompletionTokens))
	} else {
		bodyStr := string(bodyBytes)
		isStream := strings.HasPrefix(strings.TrimSpace(bodyStr), "data:") || strings.Contains(bodyStr, "\ndata:")

		if isStream {
			backendLogs = append(backendLogs, "Mendeteksi respon streaming (SSE). Memulai pemrosesan stream...")
			
			type openAIStreamDelta struct {
				Content string `json:"content"`
			}
			type openAIStreamChoice struct {
				Delta openAIStreamDelta `json:"delta"`
			}
			type openAIStreamChunk struct {
				Choices []openAIStreamChoice `json:"choices"`
			}

			var fullContent strings.Builder
			scanner := bufio.NewScanner(strings.NewReader(bodyStr))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				dataPayload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if dataPayload == "[DONE]" {
					backendLogs = append(backendLogs, "Menerima sinyal akhir stream [DONE]")
					break
				}
				var chunk openAIStreamChunk
				if err := json.Unmarshal([]byte(dataPayload), &chunk); err != nil {
					// Lewati baris yang bukan format JSON chunk valid
					continue
				}
				if len(chunk.Choices) > 0 {
					fullContent.WriteString(chunk.Choices[0].Delta.Content)
				}
			}
			rawJSONText = fullContent.String()
			backendLogs = append(backendLogs, fmt.Sprintf("Berhasil merekonstruksi %d karakter dari stream.", len(rawJSONText)))
			
			// Estimasi token usage sederhana untuk stream
			tokenUsage = &wizardTokenUsage{
				PromptTokens:     len(req.Content) / 4,
				CompletionTokens: len(rawJSONText) / 4,
				TotalTokens:      (len(req.Content) + len(rawJSONText)) / 4,
			}
		} else {
			type openAIMessage struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			type openAIChoice struct {
				Message openAIMessage `json:"message"`
			}
			type openAIUsage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}
			type openAIResp struct {
				Choices []openAIChoice `json:"choices"`
				Usage   openAIUsage    `json:"usage"`
			}

			var oResp openAIResp
			decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
			if err := decoder.Decode(&oResp); err != nil {
				writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
					Error:       "Gagal membaca struktur response OpenAI: " + err.Error(),
					BackendLogs: backendLogs,
				})
				return
			}
			if len(oResp.Choices) == 0 {
				writeJSON(w, http.StatusInternalServerError, wizardRewriteResponse{
					Error:       "Response OpenAI kosong atau tidak valid",
					BackendLogs: backendLogs,
				})
				return
			}
			rawJSONText = oResp.Choices[0].Message.Content
			tokenUsage = &wizardTokenUsage{
				PromptTokens:     oResp.Usage.PromptTokens,
				CompletionTokens: oResp.Usage.CompletionTokens,
				TotalTokens:      oResp.Usage.TotalTokens,
			}
			backendLogs = append(backendLogs, fmt.Sprintf("Mengekstrak konten dari pilihan pertama OpenAI (Token Input: %d, Output: %d)", tokenUsage.PromptTokens, tokenUsage.CompletionTokens))
		}
	}

	backendLogs = append(backendLogs, "Melakukan parsing struktur JSON hasil rewrite (title, excerpt, content)...")
	type structuredRewrite struct {
		Title   string `json:"title"`
		Excerpt string `json:"excerpt"`
		Content string `json:"content"`
	}

	cleanedJSONText := cleanJSONString(rawJSONText)
	var result structuredRewrite
	if err := json.Unmarshal([]byte(cleanedJSONText), &result); err != nil {
		backendLogs = append(backendLogs, "Peringatan: Response AI tidak berupa format JSON terstruktur yang valid. Menggunakan teks mentah sebagai konten.")
		writeJSON(w, http.StatusOK, wizardRewriteResponse{
			Title:       fmt.Sprintf("[REWRITE] %s", req.Title),
			Excerpt:     "Hasil penulisan ulang berita (Format JSON tidak terdeteksi).",
			Content:     rawJSONText,
			ModelUsed:   req.Model,
			LatencyMs:   time.Since(startTime).Milliseconds(),
			RawOutput:   rawJSONText,
			TokenUsage:  tokenUsage,
			BackendLogs: backendLogs,
		})
		return
	}

	backendLogs = append(backendLogs, "Proses rewrite selesai dengan sukses!")
	writeJSON(w, http.StatusOK, wizardRewriteResponse{
		Title:       result.Title,
		Excerpt:     result.Excerpt,
		Content:     result.Content,
		ModelUsed:   req.Model,
		LatencyMs:   time.Since(startTime).Milliseconds(),
		RawOutput:   rawJSONText,
		TokenUsage:  tokenUsage,
		BackendLogs: backendLogs,
	})
}

func cleanJSONString(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func (s *Server) wizardModels(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}

	endpointURL := strings.TrimSpace(settings["ai_endpoint_url"])
	defaultModels := []modelInfo{
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
		{ID: "gemini-1.5-flash", Name: "Gemini 1.5 Flash"},
		{ID: "gemini-1.5-pro", Name: "Gemini 1.5 Pro"},
		{ID: "mock-rewrite", Name: "Mock AI (Offline Demo)"},
	}

	if apiKey == "" {
		writeJSON(w, http.StatusOK, defaultModels)
		return
	}

	isGemini := endpointURL == "" || strings.Contains(endpointURL, "googleapis.com")
	var modelsURL string
	if isGemini {
		modelsURL = "https://generativelanguage.googleapis.com/v1beta/models?key=" + apiKey
	} else {
		baseURL := endpointURL
		baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
		baseURL = strings.TrimSuffix(baseURL, "chat/completions")
		baseURL = strings.TrimSuffix(baseURL, "/")
		modelsURL = baseURL + "/models"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, defaultModels)
		return
	}

	if !isGemini {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusOK, defaultModels)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, defaultModels)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusOK, defaultModels)
		return
	}

	var fetchedModels []modelInfo

	if isGemini {
		type geminiModelEntry struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		}
		type geminiModelsResp struct {
			Models []geminiModelEntry `json:"models"`
		}

		var gResp geminiModelsResp
		if err := json.Unmarshal(bodyBytes, &gResp); err == nil && len(gResp.Models) > 0 {
			for _, m := range gResp.Models {
				canGenerate := false
				for _, method := range m.SupportedGenerationMethods {
					if method == "generateContent" {
						canGenerate = true
						break
					}
				}
				if !canGenerate {
					continue
				}
				id := m.Name
				id = strings.TrimPrefix(id, "models/")
				fetchedModels = append(fetchedModels, modelInfo{
					ID:   id,
					Name: m.DisplayName,
				})
			}
		}
	} else {
		type openAIModelEntry struct {
			ID string `json:"id"`
		}
		type openAIModelsResp struct {
			Data []openAIModelEntry `json:"data"`
		}

		var oResp openAIModelsResp
		if err := json.Unmarshal(bodyBytes, &oResp); err == nil && len(oResp.Data) > 0 {
			for _, m := range oResp.Data {
				fetchedModels = append(fetchedModels, modelInfo{
					ID:   m.ID,
					Name: m.ID,
				})
			}
		}
	}

	if len(fetchedModels) == 0 {
		writeJSON(w, http.StatusOK, defaultModels)
		return
	}

	fetchedModels = append(fetchedModels, modelInfo{
		ID:   "mock-rewrite",
		Name: "Mock AI (Offline Demo)",
	})

	writeJSON(w, http.StatusOK, fetchedModels)
}

func parseArticleForm(r *http.Request) (cms.ArticleInput, error) {
	if err := r.ParseForm(); err != nil {
		return cms.ArticleInput{}, err
	}
	return cms.ArticleInput{Title: r.FormValue("title"), Slug: r.FormValue("slug"), Excerpt: r.FormValue("excerpt"), Content: r.FormValue("content"), Category: r.FormValue("category"), HeroImageURL: r.FormValue("hero_image_url"), Status: r.FormValue("status"), SourceURL: r.FormValue("source_url"), ImageSource: r.FormValue("image_source")}, nil
}

func articleFromInput(input cms.ArticleInput) cms.Article {
	return cms.Article{Title: input.Title, Slug: input.Slug, Excerpt: input.Excerpt, Content: input.Content, Category: input.Category, HeroImageURL: input.HeroImageURL, Status: input.Status, SourceURL: input.SourceURL, ImageSource: input.ImageSource}
}

func formStatus(article cms.Article) string {
	if article.Status == "" {
		return cms.ArticleDraft
	}
	return article.Status
}

func (s *Server) renderArticleForm(w http.ResponseWriter, data articleFormViewData) {
	if data.Status == "" {
		data.Status = cms.ArticleDraft
	}
	data.CanPublish = data.User != nil && (data.User.Role == cms.RoleAdmin || data.User.Role == cms.RoleEditor)
	data.Categories = s.store.ListCategories()
	s.renderTemplate(w, "article_form.html", data)
}

func articleFormHTML(action string, article cms.Article, message string) string {
	var err string
	if message != "" {
		err = `<div class="error">` + esc(message) + `</div>`
	}
	status := article.Status
	if status == "" {
		status = cms.ArticleDraft
	}
	return fmt.Sprintf(`%s<section class="panel"><h1>Form Artikel</h1><form method="post" action="%s"><label>Title</label><input name="title" value="%s"><label>Slug</label><input name="slug" value="%s"><label>Excerpt</label><textarea name="excerpt">%s</textarea><label>Content</label><textarea name="content">%s</textarea><label>Category</label><input name="category" value="%s"><label>Hero Image URL</label><input name="hero_image_url" value="%s"><label>Sumber Gambar</label><input name="image_source" value="%s"><label>Status</label><select name="status">%s</select><button>Simpan</button></form><form method="post" action="/dashboard/media/upload" enctype="multipart/form-data"><h2>Upload Media</h2><input type="file" name="file"><button>Upload</button></form><p><a href="/dashboard/articles">Kembali</a></p></section>`, err, esc(action), esc(article.Title), esc(article.Slug), esc(article.Excerpt), esc(article.Content), esc(article.Category), esc(article.HeroImageURL), esc(article.ImageSource), statusOptions(status))
}

func statusOptions(current string) string {
	statuses := []string{cms.ArticleDraft, cms.ArticleSubmitted, cms.ArticlePublished}
	var out strings.Builder
	for _, status := range statuses {
		selected := ""
		if status == current {
			selected = " selected"
		}
		out.WriteString(fmt.Sprintf(`<option value="%s"%s>%s</option>`, status, selected, statusLabel(status)))
	}
	return out.String()
}

func paragraphsHTML(content string) string {
	parts := strings.Split(strings.TrimSpace(content), "\n\n")
	var out strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out.WriteString(`<p>`)
			out.WriteString(esc(part))
			out.WriteString(`</p>`)
		}
	}
	return out.String()
}

func heroHTML(url string) string {
	if strings.TrimSpace(url) == "" {
		return `<div class="hero"></div>`
	}
	return `<div class="hero"><img src="` + esc(url) + `" alt="Hero artikel"></div>`
}

func dateText(t *time.Time) string {
	if t == nil {
		return "Belum publish"
	}
	return t.Format("02 Jan 2006")
}

func statusLabel(status string) string {
	return strings.ReplaceAll(status, "_", " ")
}

func valueOrEmpty(article *cms.Article) cms.Article {
	if article == nil {
		return cms.Article{}
	}
	return *article
}

func (s *Server) articleListItems(articles []cms.Article) []articleListItem {
	items := make([]articleListItem, 0, len(articles))
	for _, article := range articles {
		items = append(items, articleListItem{
			Article:    article,
			AuthorName: s.store.UserName(article.AuthorID),
		})
	}
	return items
}

func limitArticles(articles []cms.Article, limit int) []cms.Article {
	if limit <= 0 || len(articles) <= limit {
		return articles
	}
	return articles[:limit]
}

func buildHomeCategorySections(categories []cms.Category, articles []cms.Article, limitPerCategory int) []homeCategorySection {
	sections := make([]homeCategorySection, 0, len(categories))
	for _, category := range categories {
		sectionArticles := make([]cms.Article, 0, limitPerCategory)
		for _, article := range articles {
			if !strings.EqualFold(article.Category, category.Name) {
				continue
			}
			sectionArticles = append(sectionArticles, article)
			if limitPerCategory > 0 && len(sectionArticles) >= limitPerCategory {
				break
			}
		}
		if len(sectionArticles) == 0 {
			continue
		}
		section := homeCategorySection{Category: category, Articles: sectionArticles, Featured: sectionArticles[0], HasFeatured: true}
		if len(sectionArticles) > 1 {
			section.Rest = sectionArticles[1:]
		}
		sections = append(sections, section)
	}
	return sections
}

func articleStats(articles []cms.Article) (total, draft, submitted, published, today int) {
	total = len(articles)
	
	now := time.Now()
	for _, article := range articles {
		switch article.Status {
		case cms.ArticleDraft:
			draft++
		case cms.ArticleSubmitted:
			submitted++
		case cms.ArticlePublished:
			published++
		}
		
		// Check if created today (ignoring timezone issues, just comparing year, month, day of UTC / local)
		y1, m1, d1 := article.CreatedAt.Date()
		y2, m2, d2 := now.Date()
		if y1 == y2 && m1 == m2 && d1 == d2 {
			today++
		}
	}
	return total, draft, submitted, published, today
}

type duplicateArticleGroup struct {
	Type       string
	Key        string
	Similarity float64
	Articles   []cms.Article
}

type dashboardDuplicatesViewData struct {
	User          *cms.User
	UrlGroups     []duplicateArticleGroup
	TitleGroups   []duplicateArticleGroup
	SimilarGroups []duplicateArticleGroup
}

func (s *Server) dashboardDuplicates(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	t0 := time.Now()
	articles := s.store.ListArticles(user)
	s.log.Info("dashboardDuplicates: ListArticles done", "count", len(articles), "duration", time.Since(t0))

	t1 := time.Now()
	// 1. Group by exact URL
	urlMap := make(map[string][]cms.Article)
	for _, art := range articles {
		urlStr := strings.TrimSpace(art.SourceURL)
		if urlStr != "" {
			urlMap[urlStr] = append(urlMap[urlStr], art)
		}
	}
	s.log.Info("dashboardDuplicates: url grouping done", "duration", time.Since(t1))

	visited := make(map[string]bool)

	var urlGroups []duplicateArticleGroup
	for key, group := range urlMap {
		if len(group) > 1 {
			urlGroups = append(urlGroups, duplicateArticleGroup{
				Type:     "url",
				Key:      key,
				Articles: group,
			})
			for _, art := range group {
				visited[art.ID] = true
			}
		}
	}

	// 2. Group by exact Title (normalized)
	titleMap := make(map[string][]cms.Article)
	normalizeTitle := func(t string) string {
		t = strings.ToLower(t)
		var sb strings.Builder
		for _, r := range t {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				sb.WriteRune(r)
			}
		}
		return sb.String()
	}

	for _, art := range articles {
		norm := normalizeTitle(art.Title)
		if norm != "" {
			titleMap[norm] = append(titleMap[norm], art)
		}
	}

	var titleGroups []duplicateArticleGroup
	for _, group := range titleMap {
		if len(group) > 1 {
			titleGroups = append(titleGroups, duplicateArticleGroup{
				Type:     "title",
				Key:      group[0].Title,
				Articles: group,
			})
			for _, art := range group {
				visited[art.ID] = true
			}
		}
	}

	// 3. Similar Titles using Optimized Character Bigram Jaccard Similarity
	type articleRep struct {
		Article      cms.Article
		Bigrams      map[string]int
		TotalBigrams int
		NormTitle    string
		Runes        []rune
	}

	cleanRunes := func(s string) []rune {
		s = strings.ToLower(s)
		var out []rune
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				out = append(out, r)
			}
		}
		return out
	}

	getBigrams := func(runes []rune) map[string]int {
		m := make(map[string]int)
		for i := 0; i < len(runes)-1; i++ {
			bg := string(runes[i : i+2])
			m[bg]++
		}
		return m
	}

	reps := make([]articleRep, len(articles))
	for i, art := range articles {
		runes := cleanRunes(art.Title)
		reps[i] = articleRep{
			Article:      art,
			Bigrams:      getBigrams(runes),
			TotalBigrams: len(runes) - 1,
			NormTitle:    normalizeTitle(art.Title),
			Runes:        runes,
		}
	}

	var similarGroups []duplicateArticleGroup

	repBigramSimilarity := func(r1, r2 articleRep) float64 {
		if r1.TotalBigrams < 1 || r2.TotalBigrams < 1 {
			if len(r1.Runes) == len(r2.Runes) {
				match := true
				for i := range r1.Runes {
					if r1.Runes[i] != r2.Runes[i] {
						match = false
						break
					}
				}
				if match && len(r1.Runes) > 0 {
					return 1.0
				}
			}
			return 0.0
		}

		intersection := 0
		for bg, count1 := range r1.Bigrams {
			if count2, ok := r2.Bigrams[bg]; ok {
				if count1 < count2 {
					intersection += count1
				} else {
					intersection += count2
				}
			}
		}

		union := r1.TotalBigrams + r2.TotalBigrams - intersection
		if union <= 0 {
			return 0
		}

		return float64(intersection) / float64(union)
	}

	for i := 0; i < len(reps); i++ {
		if visited[reps[i].Article.ID] {
			continue
		}

		group := []cms.Article{reps[i].Article}
		maxScore := 0.0

		for j := i + 1; j < len(reps); j++ {
			if visited[reps[j].Article.ID] {
				continue
			}

			if reps[i].NormTitle == reps[j].NormTitle {
				continue
			}

			// Pruning Jaccard: if size ratio is less than 0.75, Jaccard similarity cannot be >= 0.75
			minBg := reps[i].TotalBigrams
			maxBg := reps[j].TotalBigrams
			if minBg > maxBg {
				minBg, maxBg = maxBg, minBg
			}
			if maxBg > 0 && float64(minBg) < 0.75*float64(maxBg) {
				continue
			}

			score := repBigramSimilarity(reps[i], reps[j])
			if score >= 0.75 {
				group = append(group, reps[j].Article)
				visited[reps[j].Article.ID] = true
				if score > maxScore {
					maxScore = score
				}
			}
		}

		if len(group) > 1 {
			visited[reps[i].Article.ID] = true
			similarGroups = append(similarGroups, duplicateArticleGroup{
				Type:       "similar",
				Key:        reps[i].Article.Title,
				Similarity: maxScore,
				Articles:   group,
			})
		}
	}
	s.log.Info("dashboardDuplicates: similarGroups check done", "count", len(similarGroups), "duration", time.Since(t1))

	t2 := time.Now()
	data := dashboardDuplicatesViewData{
		User:          user,
		UrlGroups:     urlGroups,
		TitleGroups:   titleGroups,
		SimilarGroups: similarGroups,
	}

	s.renderTemplate(w, "dashboard_duplicates.html", data)
	s.log.Info("dashboardDuplicates: renderTemplate done", "duration", time.Since(t2))
	s.log.Info("dashboardDuplicates: total handler execution", "duration", time.Since(t0))
}

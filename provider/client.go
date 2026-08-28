package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBaseURL  = "https://api.themoviedb.org/3"
	defaultAPIKey   = "9c18082d4985d4a204bc88af823a6353"
	maxRetries      = 3
	maxResponseBody = 1 << 20 // 1 MB
)

// Client is an HTTP client for the TMDB v3 API.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	imageBase  string // cached from /configuration
	limiter    *rate.Limiter
	configMu   sync.Mutex
}

// NewClient creates a TMDB API client with the given rate limit (requests per
// second). It uses the built-in project API key.
func NewClient(rateLimit int) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     defaultAPIKey,
		baseURL:    defaultBaseURL,
		limiter:    rate.NewLimiter(rate.Limit(rateLimit), rateLimit),
	}
}

// SetBaseURL overrides the API base URL. Used for testing.
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

// ImageURL builds a full image URL from a TMDB file_path and size.
// Returns empty string if path is empty.
func (c *Client) ImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	return c.imageBase + size + path
}

// loadConfiguration fetches /configuration and caches the image base URL.
// Safe to call multiple times. Successful loads are cached, but transient
// failures are retried so one canceled request does not poison the process.
func (c *Client) loadConfiguration(ctx context.Context) error {
	if c.imageBase != "" {
		return nil
	}

	c.configMu.Lock()
	defer c.configMu.Unlock()

	if c.imageBase != "" {
		return nil
	}

	var cfg configResponse
	if err := c.doGet(ctx, "/configuration", &cfg); err != nil {
		return err
	}
	c.imageBase = cfg.Images.SecureBaseURL
	return nil
}

// doGet executes a GET request against the TMDB API with rate limiting,
// exponential backoff on 5xx/429, and JSON decoding into dest.
func (c *Client) doGet(ctx context.Context, path string, dest any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	// Append api_key query parameter for TMDB v3 authentication.
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	reqURL := c.baseURL + path + sep + "api_key=" + url.QueryEscape(c.apiKey)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return fmt.Errorf("tmdb: create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("tmdb: request failed: %w", err)
		}

		// 429 Too Many Requests — respect Retry-After header.
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			if attempt < maxRetries {
				backoff := retryAfterOrDefault(resp, attempt)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return fmt.Errorf("tmdb: rate limited after %d retries", maxRetries)
		}

		// 5xx — retry with exponential backoff.
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt < maxRetries {
				backoff := time.Duration(1<<attempt) * time.Second
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return fmt.Errorf("tmdb: server error %d after %d retries", resp.StatusCode, maxRetries)
		}

		// 4xx — client error, no retry.
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
			resp.Body.Close()
			var apiErr apiError
			if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.StatusMessage != "" {
				return fmt.Errorf("tmdb: HTTP %d: %s", resp.StatusCode, apiErr.StatusMessage)
			}
			return fmt.Errorf("tmdb: HTTP %d", resp.StatusCode)
		}

		// 2xx — decode response.
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(dest)
		resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("tmdb: decode response: %w", decodeErr)
		}
		return nil
	}
	return fmt.Errorf("tmdb: max retries exceeded")
}

// retryAfterOrDefault parses the Retry-After header (seconds) or falls back
// to exponential backoff.
func retryAfterOrDefault(resp *http.Response, attempt int) time.Duration {
	if val := resp.Header.Get("Retry-After"); val != "" {
		if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

const maxTrendingResults = 100

func normalizeCollectionPreset(preset, mediaType, timeWindow string) (string, string, string, string, error) {
	switch preset {
	case "trending":
		switch mediaType {
		case "all", "movie", "tv":
		default:
			return "", "", "", "", fmt.Errorf("tmdb: invalid media type for preset %q: %q", preset, mediaType)
		}
		switch timeWindow {
		case "day", "week":
		default:
			return "", "", "", "", fmt.Errorf("tmdb: invalid time window for preset %q: %q", preset, timeWindow)
		}
		return preset, mediaType, timeWindow, fmt.Sprintf("/trending/%s/%s", mediaType, timeWindow), nil
	case "popular", "top_rated":
		switch mediaType {
		case "movie":
			return preset, mediaType, "", fmt.Sprintf("/movie/%s", preset), nil
		case "tv":
			return preset, mediaType, "", fmt.Sprintf("/tv/%s", preset), nil
		default:
			return "", "", "", "", fmt.Errorf("tmdb: invalid media type for preset %q: %q", preset, mediaType)
		}
	case "now_playing", "upcoming":
		if mediaType != "movie" {
			return "", "", "", "", fmt.Errorf("tmdb: preset %q requires media type %q", preset, "movie")
		}
		return preset, mediaType, "", fmt.Sprintf("/movie/%s", preset), nil
	case "airing_today", "on_the_air":
		if mediaType != "tv" {
			return "", "", "", "", fmt.Errorf("tmdb: preset %q requires media type %q", preset, "tv")
		}
		return preset, mediaType, "", fmt.Sprintf("/tv/%s", preset), nil
	default:
		return "", "", "", "", fmt.Errorf("tmdb: invalid preset: %q", preset)
	}
}

// GetTrending fetches trending items from TMDB with pagination.
// mediaType is "all", "movie", or "tv". timeWindow is "day" or "week".
// limit controls max results returned; 0 or negative defaults to 20, capped at maxTrendingResults.
func (c *Client) GetTrending(ctx context.Context, mediaType, timeWindow string, limit int) ([]TrendingResult, error) {
	switch mediaType {
	case "all", "movie", "tv":
	default:
		return nil, fmt.Errorf("tmdb: invalid media type: %q", mediaType)
	}
	switch timeWindow {
	case "day", "week":
	default:
		return nil, fmt.Errorf("tmdb: invalid time window: %q", timeWindow)
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > maxTrendingResults {
		limit = maxTrendingResults
	}

	basePath := fmt.Sprintf("/trending/%s/%s", mediaType, timeWindow)
	var all []TrendingResult

	for page := 1; len(all) < limit; page++ {
		path := fmt.Sprintf("%s?page=%d", basePath, page)
		var resp paginatedResponse[TrendingResult]
		if err := c.doGet(ctx, path, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		if page >= resp.TotalPages {
			break
		}
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// GetCollectionPreset fetches a normalized preset collection from TMDB.
func (c *Client) GetCollectionPreset(ctx context.Context, preset, mediaType, timeWindow string, limit int) ([]CollectionResult, error) {
	_, normalizedMediaType, _, basePath, err := normalizeCollectionPreset(preset, mediaType, timeWindow)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > maxTrendingResults {
		limit = maxTrendingResults
	}

	results := make([]CollectionResult, 0, limit)
	for page := 1; len(results) < limit; page++ {
		path := fmt.Sprintf("%s?page=%d", basePath, page)
		done := false

		switch preset {
		case "trending":
			var resp paginatedResponse[TrendingResult]
			if err := c.doGet(ctx, path, &resp); err != nil {
				return nil, err
			}
			for _, item := range resp.Results {
				title := item.Title
				if title == "" {
					title = item.Name
				}
				results = append(results, CollectionResult{
					ID:        item.ID,
					MediaType: item.MediaType,
					Title:     title,
				})
			}
			if page >= resp.TotalPages {
				done = true
			}
		case "popular", "top_rated", "now_playing", "upcoming":
			if normalizedMediaType == "tv" {
				var resp paginatedResponse[TVResult]
				if err := c.doGet(ctx, path, &resp); err != nil {
					return nil, err
				}
				for _, item := range resp.Results {
					results = append(results, CollectionResult{
						ID:        item.ID,
						MediaType: "tv",
						Title:     item.Name,
					})
				}
				if page >= resp.TotalPages {
					done = true
				}
				if done {
					break
				}
				continue
			}

			var resp paginatedResponse[MovieResult]
			if err := c.doGet(ctx, path, &resp); err != nil {
				return nil, err
			}
			for _, item := range resp.Results {
				results = append(results, CollectionResult{
					ID:        item.ID,
					MediaType: "movie",
					Title:     item.Title,
				})
			}
			if page >= resp.TotalPages {
				done = true
			}
		case "airing_today", "on_the_air":
			var resp paginatedResponse[TVResult]
			if err := c.doGet(ctx, path, &resp); err != nil {
				return nil, err
			}
			for _, item := range resp.Results {
				results = append(results, CollectionResult{
					ID:        item.ID,
					MediaType: "tv",
					Title:     item.Name,
				})
			}
			if page >= resp.TotalPages {
				done = true
			}
		default:
			return nil, fmt.Errorf("tmdb: invalid preset: %q", preset)
		}
		if done {
			break
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// SearchMovie searches TMDB for movies matching the query.
func (c *Client) SearchMovie(ctx context.Context, query string, year int, language string) ([]MovieResult, error) {
	path := "/search/movie?query=" + url.QueryEscape(query) + "&language=" + url.QueryEscape(language)
	if year > 0 {
		path += "&year=" + strconv.Itoa(year)
	}
	var resp paginatedResponse[MovieResult]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// SearchTV searches TMDB for TV shows matching the query.
func (c *Client) SearchTV(ctx context.Context, query string, year int, language string) ([]TVResult, error) {
	path := "/search/tv?query=" + url.QueryEscape(query) + "&language=" + url.QueryEscape(language)
	if year > 0 {
		path += "&first_air_date_year=" + strconv.Itoa(year)
	}
	var resp paginatedResponse[TVResult]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// FindByExternalID looks up a TMDB entity by an external ID (e.g. IMDb,
// TVDB). The source parameter must be one of: "imdb_id", "tvdb_id".
func (c *Client) FindByExternalID(ctx context.Context, externalID, source string) (*FindResponse, error) {
	path := "/find/" + url.PathEscape(externalID) + "?external_source=" + url.QueryEscape(source)
	var resp FindResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetMovie fetches full movie details with credits, external IDs, images,
// keywords, release dates, and videos appended in a single API call.
// The lang parameter is an ISO 639-1 code (e.g. "en", "ja") passed as
// ?language= and &include_image_language= to get localized metadata and images.
// Videos fall back to English and language-less entries because TMDB only
// returns videos matching the request language by default.
func (c *Client) GetMovie(ctx context.Context, id int, lang string) (*MovieDetail, error) {
	path := fmt.Sprintf("/movie/%d?append_to_response=credits,external_ids,images,keywords,release_dates,alternative_titles,videos&language=%s&include_image_language=%s,null&include_video_language=%s,en,null",
		id, url.QueryEscape(lang), url.QueryEscape(lang), url.QueryEscape(lang))
	var movie MovieDetail
	if err := c.doGet(ctx, path, &movie); err != nil {
		return nil, err
	}
	return &movie, nil
}

// GetTV fetches full TV show details with credits, external IDs, images,
// keywords, content ratings, and videos appended.
func (c *Client) GetTV(ctx context.Context, id int, lang string) (*TVDetail, error) {
	path := fmt.Sprintf("/tv/%d?append_to_response=credits,external_ids,images,keywords,content_ratings,alternative_titles,videos&language=%s&include_image_language=%s,null&include_video_language=%s,en,null",
		id, url.QueryEscape(lang), url.QueryEscape(lang), url.QueryEscape(lang))
	var tv TVDetail
	if err := c.doGet(ctx, path, &tv); err != nil {
		return nil, err
	}
	return &tv, nil
}

// GetTVSeasonImages fetches every image attached to one exact TV season.
// include_image_language carries the requested language, English fallback,
// and language-neutral artwork in a single provider request.
func (c *Client) GetTVSeasonImages(ctx context.Context, tvID, seasonNumber int, lang string) (*ImageSet, error) {
	path := fmt.Sprintf("/tv/%d/season/%d/images?language=%s&include_image_language=%s",
		tvID, seasonNumber, url.QueryEscape(lang), url.QueryEscape(tmdbImageLanguages(lang)))
	var images ImageSet
	if err := c.doGet(ctx, path, &images); err != nil {
		return nil, err
	}
	return &images, nil
}

// GetPerson fetches full person details with external IDs appended.
func (c *Client) GetPerson(ctx context.Context, id int, lang string) (*PersonDetail, error) {
	path := fmt.Sprintf("/person/%d?append_to_response=external_ids&language=%s", id, url.QueryEscape(lang))
	var person PersonDetail
	if err := c.doGet(ctx, path, &person); err != nil {
		return nil, err
	}
	return &person, nil
}

// GetSeason fetches a TV season with all episodes inline.
func (c *Client) GetSeason(ctx context.Context, tvID, seasonNumber int, lang string) (*SeasonDetail, error) {
	path := fmt.Sprintf("/tv/%d/season/%d?language=%s", tvID, seasonNumber, url.QueryEscape(lang))
	var season SeasonDetail
	if err := c.doGet(ctx, path, &season); err != nil {
		return nil, err
	}
	return &season, nil
}

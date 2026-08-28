package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Silo-Server/silo-plugin-tmdb/metadata"
)

func TestTMDBLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "en-US"},
		{input: "en", want: "en-US"},
		{input: "en-US", want: "en-US"},
		{input: "en_us", want: "en-US"},
		{input: "fr", want: "fr"},
		{input: "fr-CA", want: "fr-CA"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := tmdbLanguage(tt.input); got != tt.want {
				t.Fatalf("tmdbLanguage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTMDBLocalizedRequestsUseEnglishUSForEnglish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		path            string
		expectImageLang bool
		response        map[string]any
		invoke          func(context.Context, *Provider) error
	}{
		{
			name:            "movie metadata",
			path:            "/movie/42",
			expectImageLang: true,
			response: map[string]any{
				"id":                   42,
				"title":                "Movie",
				"overview":             "Overview",
				"original_language":    "ko",
				"genres":               []any{},
				"production_companies": []any{},
				"credits": map[string]any{
					"cast": []any{},
					"crew": []any{},
				},
				"external_ids": map[string]any{},
				"images":       map[string]any{},
				"release_dates": map[string]any{
					"results": []any{},
				},
			},
			invoke: func(ctx context.Context, p *Provider) error {
				_, err := p.GetMetadata(ctx, metadata.MetadataRequest{
					ProviderIDs: map[string]string{"tmdb": "42"},
					ContentType: "movie",
					Language:    "en",
				})
				return err
			},
		},
		{
			name:            "series metadata",
			path:            "/tv/77",
			expectImageLang: true,
			response: map[string]any{
				"id":                77,
				"name":              "Series",
				"overview":          "Overview",
				"original_language": "ko",
				"genres":            []any{},
				"networks":          []any{},
				"seasons":           []any{},
				"credits": map[string]any{
					"cast": []any{},
					"crew": []any{},
				},
				"external_ids": map[string]any{},
				"images":       map[string]any{},
				"content_ratings": map[string]any{
					"results": []any{},
				},
			},
			invoke: func(ctx context.Context, p *Provider) error {
				_, err := p.GetMetadata(ctx, metadata.MetadataRequest{
					ProviderIDs: map[string]string{"tmdb": "77"},
					ContentType: "series",
					Language:    "en",
				})
				return err
			},
		},
		{
			name:            "images",
			path:            "/movie/42",
			expectImageLang: true,
			response: map[string]any{
				"id":     42,
				"images": map[string]any{},
			},
			invoke: func(ctx context.Context, p *Provider) error {
				_, err := p.GetImages(ctx, metadata.ImageRequest{
					ProviderIDs: map[string]string{"tmdb": "42"},
					ContentType: "movie",
					Language:    "en",
				})
				return err
			},
		},
		{
			name:            "person detail",
			path:            "/person/287",
			expectImageLang: false,
			response: map[string]any{
				"id":           287,
				"name":         "Brad Pitt",
				"external_ids": map[string]any{},
			},
			invoke: func(ctx context.Context, p *Provider) error {
				_, err := p.GetPersonDetail(ctx, metadata.PersonDetailRequest{
					ProviderIDs: map[string]string{"tmdb": "287"},
					Language:    "en",
				})
				return err
			},
		},
		{
			name:            "seasons",
			path:            "/tv/77",
			expectImageLang: true,
			response: map[string]any{
				"id":      77,
				"seasons": []any{},
			},
			invoke: func(ctx context.Context, p *Provider) error {
				_, err := p.GetSeasons(ctx, metadata.SeasonsRequest{
					ProviderIDs: map[string]string{"tmdb": "77"},
					ContentType: "series",
					Language:    "en",
				})
				return err
			},
		},
		{
			name:            "episodes",
			path:            "/tv/77/season/2",
			expectImageLang: false,
			response: map[string]any{
				"id":       2,
				"episodes": []any{},
			},
			invoke: func(ctx context.Context, p *Provider) error {
				_, err := p.GetEpisodes(ctx, metadata.EpisodesRequest{
					ProviderIDs:  map[string]string{"tmdb": "77"},
					SeasonNumber: 2,
					Language:     "en",
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				switch r.URL.Path {
				case "/configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"images": map[string]any{
							"secure_base_url": serverURL(t, r) + "/images/",
						},
					})
				case tt.path:
					if got := r.URL.Query().Get("language"); got != "en-US" {
						t.Fatalf("language = %q, want en-US", got)
					}
					if tt.expectImageLang {
						if got := r.URL.Query().Get("include_image_language"); got != "en-US,null" {
							t.Fatalf("include_image_language = %q, want en-US,null", got)
						}
					}
					_ = json.NewEncoder(w).Encode(tt.response)
				default:
					t.Fatalf("unexpected path: %s", r.URL.String())
				}
			}))
			defer server.Close()

			p := newTMDBTestProvider(server.URL)
			if err := tt.invoke(context.Background(), p); err != nil {
				t.Fatalf("invoke error = %v", err)
			}
		})
	}
}

func TestSearchReturnsLocalizedAndOriginalTitles(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{"secure_base_url": serverURL(t, r) + "/images/"},
			})
		case "/search/movie":
			if got := r.URL.Query().Get("query"); got != "Ten Tricks" {
				t.Fatalf("query = %q, want Ten Tricks", got)
			}
			if got := r.URL.Query().Get("language"); got != "en-US" {
				t.Fatalf("language = %q, want en-US", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":                42,
					"title":             "10 Tricks",
					"original_title":    "Dieci trucchi",
					"original_language": "it",
					"release_date":      "2022-01-01",
				}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	results, err := newTMDBTestProvider(server.URL).Search(context.Background(), metadata.SearchQuery{
		Title:       "Ten Tricks",
		Year:        2022,
		ContentType: "movie",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	got := results[0]
	if got.Name != "10 Tricks" || got.OriginalTitle != "Dieci trucchi" {
		t.Fatalf("titles = (%q, %q)", got.Name, got.OriginalTitle)
	}
	if got.TitleLanguage != "en" || got.TitleIsFallback || got.OriginalLanguage != "it" {
		t.Fatalf("language metadata = (%q, %v, %q)", got.TitleLanguage, got.TitleIsFallback, got.OriginalLanguage)
	}
	if len(got.TitleAliases) != 1 || got.TitleAliases[0].Title != "Dieci trucchi" || got.TitleAliases[0].Kind != "original" {
		t.Fatalf("aliases = %#v, want original title", got.TitleAliases)
	}
}

func TestGetMetadataReturnsAlternativeTitlesAndMarksNativeFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{"secure_base_url": serverURL(t, r) + "/images/"},
			})
		case "/tv/77":
			if got := r.URL.Query().Get("language"); got != "en-US" {
				t.Fatalf("language = %q, want en-US", got)
			}
			if appended := r.URL.Query().Get("append_to_response"); !strings.Contains(appended, "alternative_titles") {
				t.Fatalf("append_to_response = %q, want alternative_titles", appended)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 77, "name": "倒凶十将伝", "original_name": "倒凶十将伝", "original_language": "ja",
				"first_air_date": "1999-01-01", "genres": []any{}, "networks": []any{}, "seasons": []any{},
				"credits": map[string]any{"cast": []any{}, "crew": []any{}}, "external_ids": map[string]any{},
				"images": map[string]any{}, "content_ratings": map[string]any{"results": []any{}},
				"alternative_titles": map[string]any{"results": []map[string]any{{"title": "10 Tokyo Warriors"}}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	result, err := newTMDBTestProvider(server.URL).GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tmdb": "77"}, ContentType: "series", Language: "en",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if result == nil || !result.TitleIsFallback || result.TitleLanguage != "ja" {
		t.Fatalf("fallback title metadata = %#v", result)
	}
	if !result.TitleAliasesComplete {
		t.Fatal("full TMDB detail response must mark title aliases complete")
	}
	found := false
	for _, alias := range result.TitleAliases {
		found = found || alias.Title == "10 Tokyo Warriors" && alias.Kind == "alternate" && alias.Language == ""
	}
	if !found {
		t.Fatalf("aliases = %#v, want unknown-language alternative title", result.TitleAliases)
	}
}

func TestGetMetadataMovieReturnsSortedVideos(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/movie/42":
			appended := r.URL.Query().Get("append_to_response")
			if !strings.Contains(appended, "videos") {
				t.Errorf("append_to_response = %q, want videos included", appended)
			}
			if !strings.Contains(appended, "alternative_titles") {
				t.Errorf("append_to_response = %q, want alternative_titles preserved", appended)
			}
			if got := r.URL.Query().Get("include_video_language"); got != "en-US,en,null" {
				t.Errorf("include_video_language = %q, want en-US,en,null", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    42,
				"title": "Movie",
				"videos": map[string]any{
					"results": []map[string]any{
						{"id": "clip-1", "iso_639_1": "en", "key": "clipKey", "name": "A Clip", "site": "YouTube", "size": 720, "type": "Clip", "official": true},
						{"id": "fan-trailer", "iso_639_1": "en", "key": "fanKey", "name": "Fan Trailer", "site": "YouTube", "size": 1080, "type": "Trailer", "official": false},
						{"id": "bts-1", "iso_639_1": "en", "key": "btsKey", "name": "Making Of", "site": "Vimeo", "size": 1080, "type": "Behind the Scenes", "official": true},
						{"id": "broken", "site": "YouTube", "type": "Trailer"},
						{"id": "official-trailer", "iso_639_1": "en", "key": "offKey", "name": "Official Trailer", "site": "YouTube", "size": 2160, "type": "Trailer", "official": true, "published_at": "2024-01-15T00:00:00.000Z"},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tmdb": "42"},
		ContentType: "movie",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}

	// The entry without a key is dropped; official trailers sort first, then
	// other trailers, then the rest in TMDB order.
	wantOrder := []string{"official-trailer", "fan-trailer", "clip-1", "bts-1"}
	if len(result.Videos) != len(wantOrder) {
		t.Fatalf("len(Videos) = %d, want %d", len(result.Videos), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got := result.Videos[i].ProviderKey; got != want {
			t.Fatalf("Videos[%d].ProviderKey = %q, want %q", i, got, want)
		}
	}

	want := metadata.VideoResult{
		ProviderKey: "official-trailer",
		Kind:        "trailer",
		Site:        "youtube",
		SiteKey:     "offKey",
		Name:        "Official Trailer",
		Language:    "en",
		IsOfficial:  true,
		SizeHint:    2160,
		PublishedAt: "2024-01-15T00:00:00.000Z",
	}
	if result.Videos[0] != want {
		t.Fatalf("Videos[0] = %+v, want %+v", result.Videos[0], want)
	}

	if got := result.Videos[3]; got.Kind != "behind_the_scenes" || got.Site != "vimeo" {
		t.Fatalf("Videos[3] kind/site = %q/%q, want behind_the_scenes/vimeo", got.Kind, got.Site)
	}
}

func TestGetMetadataSeriesReturnsVideos(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/tv/77":
			if got := r.URL.Query().Get("append_to_response"); !strings.Contains(got, "videos") {
				t.Errorf("append_to_response = %q, want videos included", got)
			}
			if got := r.URL.Query().Get("include_video_language"); got != "en-US,en,null" {
				t.Errorf("include_video_language = %q, want en-US,en,null", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   77,
				"name": "Series",
				"videos": map[string]any{
					"results": []map[string]any{
						{"id": "s-teaser", "iso_639_1": "en", "key": "teaserKey", "name": "Season Teaser", "site": "YouTube", "size": 1080, "type": "Teaser", "official": true},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tmdb": "77"},
		ContentType: "series",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}

	if len(result.Videos) != 1 {
		t.Fatalf("len(Videos) = %d, want 1", len(result.Videos))
	}
	if got := result.Videos[0]; got.Kind != "teaser" || got.SiteKey != "teaserKey" {
		t.Fatalf("Videos[0] = %+v, want teaser/teaserKey", got)
	}
}

func TestGetImagesReturnsRawPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42,
				"images": map[string]any{
					"posters": []map[string]any{
						{"file_path": "/poster.jpg", "width": 2000, "height": 3000, "vote_average": 8.0},
					},
					"backdrops": []map[string]any{
						{"file_path": "/backdrop.jpg", "width": 3840, "height": 2160, "vote_average": 7.0},
					},
					"logos": []map[string]any{
						{"file_path": "/logo.png", "width": 1200, "height": 600, "vote_average": 6.0},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tmdb": "42"},
		ContentType: "movie",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 3 {
		t.Fatalf("len(images) = %d, want 3", len(images))
	}

	got := map[metadata.ImageType]metadata.RemoteImage{}
	for _, img := range images {
		got[img.Type] = img
	}

	if got[metadata.ImagePoster].URL != "/poster.jpg" {
		t.Fatalf("poster URL = %q", got[metadata.ImagePoster].URL)
	}
	if got[metadata.ImagePoster].IncludesText == nil || *got[metadata.ImagePoster].IncludesText {
		t.Fatalf("poster IncludesText = %v, want false for language-neutral art", got[metadata.ImagePoster].IncludesText)
	}
	if got[metadata.ImageBackdrop].URL != "/backdrop.jpg" {
		t.Fatalf("backdrop URL = %q", got[metadata.ImageBackdrop].URL)
	}
	if got[metadata.ImageBackdrop].IncludesText == nil || *got[metadata.ImageBackdrop].IncludesText {
		t.Fatalf("backdrop IncludesText = %v, want false for language-neutral art", got[metadata.ImageBackdrop].IncludesText)
	}
	if got[metadata.ImageLogo].URL != "/logo.png" {
		t.Fatalf("logo URL = %q", got[metadata.ImageLogo].URL)
	}
	if got[metadata.ImageLogo].IncludesText == nil || *got[metadata.ImageLogo].IncludesText {
		t.Fatalf("logo IncludesText = %v, want false for language-neutral art", got[metadata.ImageLogo].IncludesText)
	}
}

func TestGetImagesTreatsNoLanguageCodeAsTextless(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42,
				"images": map[string]any{
					"posters": []map[string]any{
						{"file_path": "/poster-xx.jpg", "iso_639_1": "xx", "vote_average": 8.0},
					},
					"backdrops": []map[string]any{
						{"file_path": "/backdrop-en.jpg", "iso_639_1": "en", "vote_average": 7.0},
					},
					"logos": []map[string]any{
						{"file_path": "/logo-en.png", "iso_639_1": "en", "vote_average": 6.0},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	images, err := newTMDBTestProvider(server.URL).GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tmdb": "42"},
		ContentType: "movie",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}

	got := map[metadata.ImageType]metadata.RemoteImage{}
	for _, img := range images {
		got[img.Type] = img
	}

	poster := got[metadata.ImagePoster]
	if poster.IncludesText == nil || *poster.IncludesText {
		t.Fatalf("poster IncludesText = %v, want false for TMDB \"xx\" (No Language)", poster.IncludesText)
	}
	backdrop := got[metadata.ImageBackdrop]
	if backdrop.IncludesText == nil || !*backdrop.IncludesText {
		t.Fatalf("backdrop IncludesText = %v, want true for language-tagged art", backdrop.IncludesText)
	}
	logo := got[metadata.ImageLogo]
	if logo.IncludesText == nil || !*logo.IncludesText {
		t.Fatalf("logo IncludesText = %v, want true for language-tagged art", logo.IncludesText)
	}
}

func TestGetImagesReturnsExactSeasonGallery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{"secure_base_url": serverURL(t, r) + "/images/"},
			})
		case "/tv/42/season/0/images":
			if got := r.URL.Query().Get("language"); got != "fr" {
				t.Fatalf("language = %q, want fr", got)
			}
			if got := r.URL.Query().Get("include_image_language"); got != "fr,en,null" {
				t.Fatalf("include_image_language = %q, want fr,en,null", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"posters": []map[string]any{
					{"file_path": "/specials-fr.jpg", "iso_639_1": "fr", "width": 2000, "height": 3000, "vote_average": 8.0},
					{"file_path": "/specials-en.jpg", "iso_639_1": "en", "width": 2000, "height": 3000, "vote_average": 7.0},
				},
				"backdrops": []map[string]any{{"file_path": "/show-backdrop.jpg"}},
				"logos":     []map[string]any{{"file_path": "/show-logo.png"}},
			})
		case "/tv/42/season/0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          9,
				"poster_path": "/specials-en.jpg",
				"episodes":    []any{},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	specials := 0
	images, err := newTMDBTestProvider(server.URL).GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs:  map[string]string{"tmdb": "42"},
		ContentType:  "series",
		Language:     "fr",
		SeasonNumber: &specials,
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("images = %#v, want two season posters only", images)
	}
	for _, image := range images {
		if image.Type != metadata.ImagePoster {
			t.Fatalf("image type = %v, want poster", image.Type)
		}
		if image.SeasonNumber == nil || *image.SeasonNumber != 0 {
			t.Fatalf("image SeasonNumber = %v, want present Specials value 0", image.SeasonNumber)
		}
	}
	if images[0].URL != "/specials-fr.jpg" || images[1].URL != "/specials-en.jpg" {
		t.Fatalf("images = %#v, want exact Specials gallery", images)
	}
	if images[0].Rating != 8.0 {
		t.Fatalf("unboosted rating = %v, want 8.0", images[0].Rating)
	}
	if images[1].Rating != 9.0 {
		t.Fatalf("season primary poster rating = %v, want 9.0", images[1].Rating)
	}
}

func TestGetImagesKeepsSeasonGalleryWhenSeasonLookupFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{"secure_base_url": serverURL(t, r) + "/images/"},
			})
		case "/tv/42/season/3/images":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"posters": []map[string]any{
					{"file_path": "/season-three.jpg", "iso_639_1": "fr", "vote_average": 8.0},
				},
			})
		case "/tv/42/season/3":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	seasonNumber := 3
	images, err := newTMDBTestProvider(server.URL).GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs:  map[string]string{"tmdb": "42"},
		ContentType:  "series",
		Language:     "fr",
		SeasonNumber: &seasonNumber,
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("images = %#v, want the single season poster", images)
	}
	if images[0].URL != "/season-three.jpg" {
		t.Fatalf("images[0].URL = %q, want /season-three.jpg", images[0].URL)
	}
	if images[0].Rating != 8.0 {
		t.Fatalf("images[0].Rating = %v, want unboosted 8.0", images[0].Rating)
	}
}

func TestGetImagesScopesEnglishFallbackToSeasonGalleries(t *testing.T) {
	t.Parallel()

	seasonThree := 3
	tests := []struct {
		name        string
		path        string
		contentType string
		wantLang    string
		response    map[string]any
		request     metadata.ImageRequest
	}{
		{
			name:     "movie",
			path:     "/movie/42",
			wantLang: "fr,null",
			response: map[string]any{"id": 42, "images": map[string]any{}},
			request: metadata.ImageRequest{
				ProviderIDs: map[string]string{"tmdb": "42"},
				ContentType: "movie",
				Language:    "fr",
			},
		},
		{
			name:     "series",
			path:     "/tv/42",
			wantLang: "fr,null",
			response: map[string]any{"id": 42, "images": map[string]any{}},
			request: metadata.ImageRequest{
				ProviderIDs: map[string]string{"tmdb": "42"},
				ContentType: "series",
				Language:    "fr",
			},
		},
		{
			name:     "season gallery",
			path:     "/tv/42/season/3/images",
			wantLang: "fr,en,null",
			response: map[string]any{},
			request: metadata.ImageRequest{
				ProviderIDs:  map[string]string{"tmdb": "42"},
				ContentType:  "series",
				Language:     "fr",
				SeasonNumber: &seasonThree,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				switch r.URL.Path {
				case "/configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"images": map[string]any{"secure_base_url": serverURL(t, r) + "/images/"},
					})
				case "/tv/42/season/3":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "episodes": []any{}})
				case tt.path:
					if got := r.URL.Query().Get("include_image_language"); got != tt.wantLang {
						t.Fatalf("include_image_language = %q, want %q", got, tt.wantLang)
					}
					_ = json.NewEncoder(w).Encode(tt.response)
				default:
					t.Fatalf("unexpected path: %s", r.URL.String())
				}
			}))
			defer server.Close()

			if _, err := newTMDBTestProvider(server.URL).GetImages(context.Background(), tt.request); err != nil {
				t.Fatalf("GetImages() error = %v", err)
			}
		})
	}
}

func TestGetImagesPrefersTMDBPrimaryPoster(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          42,
				"poster_path": "/poster-primary.jpg",
				"images": map[string]any{
					"posters": []map[string]any{
						{
							"file_path":    "/poster-primary.jpg",
							"iso_639_1":    "en",
							"width":        2000,
							"height":       3000,
							"vote_average": 5.0,
						},
						{
							"file_path":    "/poster-textless.jpg",
							"iso_639_1":    nil,
							"width":        2000,
							"height":       3000,
							"vote_average": 8.0,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tmdb": "42"},
		ContentType: "movie",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}

	var primary, textless *metadata.RemoteImage
	for i := range images {
		switch images[i].URL {
		case "/poster-primary.jpg":
			primary = &images[i]
		case "/poster-textless.jpg":
			textless = &images[i]
		}
	}

	if primary == nil {
		t.Fatal("primary poster missing from GetImages() result")
	}
	if textless == nil {
		t.Fatal("textless poster missing from GetImages() result")
	}
	if primary.IncludesText == nil || !*primary.IncludesText {
		t.Fatalf("primary IncludesText = %v, want true", primary.IncludesText)
	}
	if textless.IncludesText == nil || *textless.IncludesText {
		t.Fatalf("textless IncludesText = %v, want false", textless.IncludesText)
	}
	if primary.Rating <= textless.Rating {
		t.Fatalf("primary rating = %v, textless rating = %v; want primary > textless", primary.Rating, textless.Rating)
	}
}

func TestGetImagesPrimaryBoostPreservesTextlessSignal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{"secure_base_url": serverURL(t, r) + "/images/"},
			})
		case "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          42,
				"poster_path": "/poster-textless.jpg",
				"images": map[string]any{
					"posters": []map[string]any{
						{"file_path": "/poster-textless.jpg", "iso_639_1": nil, "vote_average": 8.0},
						{"file_path": "/poster-english.jpg", "iso_639_1": "en", "vote_average": 7.0},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	images, err := newTMDBTestProvider(server.URL).GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tmdb": "42"},
		ContentType: "movie",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	for i := range images {
		if images[i].URL != "/poster-textless.jpg" {
			continue
		}
		if images[i].Language != "en" {
			t.Fatalf("primary language = %q, want boosted request language en", images[i].Language)
		}
		if images[i].IncludesText == nil || *images[i].IncludesText {
			t.Fatalf("primary IncludesText = %v, want false", images[i].IncludesText)
		}
		return
	}
	t.Fatal("primary textless poster missing")
}

func TestGetImagesAddsPrimaryPosterWhenImagesMissIt(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          42,
				"poster_path": "/poster-primary.jpg",
				"images": map[string]any{
					"posters": []map[string]any{
						{
							"file_path":    "/poster-alt.jpg",
							"width":        2000,
							"height":       3000,
							"vote_average": 8.0,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tmdb": "42"},
		ContentType: "movie",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}

	var primary, alt *metadata.RemoteImage
	for i := range images {
		switch images[i].URL {
		case "/poster-primary.jpg":
			primary = &images[i]
		case "/poster-alt.jpg":
			alt = &images[i]
		}
	}

	if primary == nil {
		t.Fatal("primary poster was not appended to GetImages() result")
	}
	if alt == nil {
		t.Fatal("alternate poster missing from GetImages() result")
	}
	if primary.Language != "en" {
		t.Fatalf("primary language = %q, want en", primary.Language)
	}
	if primary.IncludesText == nil || !*primary.IncludesText {
		t.Fatalf("primary IncludesText = %v, want true for language-selected primary", primary.IncludesText)
	}
	if alt.IncludesText == nil || *alt.IncludesText {
		t.Fatalf("alt IncludesText = %v, want false for language-neutral art", alt.IncludesText)
	}
	if primary.Rating <= alt.Rating {
		t.Fatalf("primary rating = %v, alt rating = %v; want primary > alt", primary.Rating, alt.Rating)
	}
}

func TestGetSeasonsReturnsRawPosterPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/tv/77":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 77,
				"seasons": []map[string]any{
					{"season_number": 2, "poster_path": "/season-two.jpg"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	seasons, err := p.GetSeasons(context.Background(), metadata.SeasonsRequest{
		ProviderIDs: map[string]string{"tmdb": "77"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("GetSeasons() error = %v", err)
	}
	if len(seasons) != 1 {
		t.Fatalf("len(seasons) = %d, want 1", len(seasons))
	}
	if seasons[0].PosterPath != "/season-two.jpg" {
		t.Fatalf("season poster = %q", seasons[0].PosterPath)
	}
}

func TestGetEpisodesReturnsRawStillPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/tv/77/season/2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 2,
				"episodes": []map[string]any{
					{
						"id":             9001,
						"season_number":  2,
						"episode_number": 5,
						"name":           "Test Episode",
						"still_path":     "/still.jpg",
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"tmdb": "77"},
		SeasonNumber: 2,
	})
	if err != nil {
		t.Fatalf("GetEpisodes() error = %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("len(episodes) = %d, want 1", len(episodes))
	}
	if episodes[0].StillPath != "/still.jpg" {
		t.Fatalf("episode still = %q", episodes[0].StillPath)
	}
}

func TestGetPersonDetail_UsesTMDBID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/person/287":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             287,
				"name":           "Brad Pitt",
				"biography":      "Biography",
				"birthday":       "1963-12-18",
				"place_of_birth": "Shawnee, Oklahoma, USA",
				"homepage":       "https://example.test/brad",
				"profile_path":   "/brad.jpg",
				"external_ids": map[string]any{
					"imdb_id": "nm0000093",
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	person, err := p.GetPersonDetail(context.Background(), metadata.PersonDetailRequest{
		ProviderIDs: map[string]string{"tmdb": "287"},
	})
	if err != nil {
		t.Fatalf("GetPersonDetail() error = %v", err)
	}
	if person == nil {
		t.Fatal("GetPersonDetail() returned nil person")
	}
	if person.Name != "Brad Pitt" {
		t.Fatalf("Name = %q, want Brad Pitt", person.Name)
	}
	if person.BirthDate != "1963-12-18" {
		t.Fatalf("BirthDate = %q, want 1963-12-18", person.BirthDate)
	}
	if person.Birthplace != "Shawnee, Oklahoma, USA" {
		t.Fatalf("Birthplace = %q", person.Birthplace)
	}
	if person.Homepage != "https://example.test/brad" {
		t.Fatalf("Homepage = %q", person.Homepage)
	}
	if person.PhotoPath != "/brad.jpg" {
		t.Fatalf("PhotoPath = %q, want /brad.jpg", person.PhotoPath)
	}
	if person.ProviderIDs["tmdb"] != "287" {
		t.Fatalf("ProviderIDs[tmdb] = %q, want 287", person.ProviderIDs["tmdb"])
	}
	if person.ProviderIDs["imdb"] != "nm0000093" {
		t.Fatalf("ProviderIDs[imdb] = %q, want nm0000093", person.ProviderIDs["imdb"])
	}
}

func TestGetPersonDetail_FindsTMDBPersonByIMDbID(t *testing.T) {
	t.Parallel()

	findSource := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/find/nm0000093":
			findSource = r.URL.Query().Get("external_source")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"person_results": []map[string]any{
					{"id": 287},
				},
			})
		case "/person/287":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           287,
				"name":         "Brad Pitt",
				"profile_path": "/brad.jpg",
				"external_ids": map[string]any{
					"imdb_id": "nm0000093",
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)

	person, err := p.GetPersonDetail(context.Background(), metadata.PersonDetailRequest{
		ProviderIDs: map[string]string{"imdb": "nm0000093"},
	})
	if err != nil {
		t.Fatalf("GetPersonDetail() error = %v", err)
	}
	if person == nil {
		t.Fatal("GetPersonDetail() returned nil person")
	}
	if person.ProviderIDs["tmdb"] != "287" {
		t.Fatalf("ProviderIDs[tmdb] = %q, want 287", person.ProviderIDs["tmdb"])
	}
	if findSource != "imdb_id" {
		t.Fatalf("external_source = %q, want imdb_id", findSource)
	}
}

func newTMDBTestProvider(baseURL string) *Provider {
	client := NewClient(1000)
	client.SetBaseURL(baseURL)
	return NewProviderWithClient(client)
}

func serverURL(t *testing.T, r *http.Request) string {
	t.Helper()
	return "http://" + r.Host
}

func TestGetTVMetadataCarriesShowStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": serverURL(t, r) + "/images/",
				},
			})
		case "/tv/77":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                77,
				"name":              "Series",
				"overview":          "Overview",
				"original_language": "en",
				"status":            "Returning Series",
				"genres":            []any{},
				"networks":          []any{},
				"seasons":           []any{},
				"credits": map[string]any{
					"cast": []any{},
					"crew": []any{},
				},
				"external_ids": map[string]any{},
				"images":       map[string]any{},
				"content_ratings": map[string]any{
					"results": []any{},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	p := newTMDBTestProvider(server.URL)
	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tmdb": "77"},
		ContentType: "series",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if result == nil {
		t.Fatal("GetMetadata() returned nil result")
	}
	if result.ShowStatus != "Returning Series" {
		t.Fatalf("ShowStatus = %q, want %q", result.ShowStatus, "Returning Series")
	}
}

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-tmdb/metadata"
	"github.com/Silo-Server/silo-plugin-tmdb/provider"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRuntimeServerConfigure_NoOp(t *testing.T) {
	server := &runtimeServer{provider: provider.NewProvider()}

	_, err := server.Configure(context.Background(), &pluginv1.ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() returned error: %v", err)
	}

	p, err := server.providerForRequest()
	if err != nil {
		t.Fatalf("providerForRequest() returned error: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider to be available")
	}
}

func TestImageRecordMetadataIncludesArtworkTextSignal(t *testing.T) {
	includesText := false
	md := imageRecordMetadata(metadata.RemoteImage{
		Rating:       8.5,
		IncludesText: &includesText,
	})
	if md == nil {
		t.Fatal("imageRecordMetadata() = nil")
	}
	if got := md.GetFields()["rating"].GetNumberValue(); got != 8.5 {
		t.Fatalf("rating = %v, want 8.5", got)
	}
	includesTextField, ok := md.GetFields()["includes_text"]
	if !ok {
		t.Fatal("includes_text metadata is missing")
	}
	if includesTextField.GetBoolValue() {
		t.Fatal("includes_text = true, want false")
	}
}

func mustStruct(t *testing.T, value map[string]any) *structpb.Struct {
	t.Helper()

	result, err := structpb.NewStruct(value)
	if err != nil {
		t.Fatalf("structpb.NewStruct() returned error: %v", err)
	}
	return result
}

func metadataItemReleaseDate(t *testing.T, item *pluginv1.MetadataItem) string {
	t.Helper()

	field := item.ProtoReflect().Descriptor().Fields().ByName("release_date")
	if field == nil {
		t.Fatal("MetadataItem descriptor is missing release_date")
	}

	value := item.ProtoReflect().Get(field)
	if value.Interface() == nil {
		return ""
	}
	return value.String()
}

func TestResolveImageURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		variant string
		wantURL string
	}{
		// poster variants
		{name: "poster card", path: "tmdb://poster/poster.jpg", variant: "card", wantURL: "https://image.tmdb.org/t/p/w300/poster.jpg"},
		{name: "poster featured", path: "tmdb://poster/poster.jpg", variant: "featured", wantURL: "https://image.tmdb.org/t/p/w500/poster.jpg"},
		{name: "poster full", path: "tmdb://poster/poster.jpg", variant: "full", wantURL: "https://image.tmdb.org/t/p/w780/poster.jpg"},
		{name: "poster original", path: "tmdb://poster/poster.jpg", variant: "original", wantURL: "https://image.tmdb.org/t/p/original/poster.jpg"},
		{name: "poster empty variant", path: "tmdb://poster/poster.jpg", variant: "", wantURL: "https://image.tmdb.org/t/p/original/poster.jpg"},
		// backdrop variants
		{name: "backdrop featured", path: "tmdb://backdrop/backdrop.jpg", variant: "featured", wantURL: "https://image.tmdb.org/t/p/w1280/backdrop.jpg"},
		{name: "backdrop card", path: "tmdb://backdrop/backdrop.jpg", variant: "card", wantURL: "https://image.tmdb.org/t/p/w300/backdrop.jpg"},
		// still variants
		{name: "still card", path: "tmdb://still/still.jpg", variant: "card", wantURL: "https://image.tmdb.org/t/p/w300/still.jpg"},
		// logo variants
		{name: "logo featured", path: "tmdb://logo/logo.png", variant: "featured", wantURL: "https://image.tmdb.org/t/p/w500/logo.png"},
		// profile variants
		{name: "profile card", path: "tmdb://profile/person.jpg", variant: "card", wantURL: "https://image.tmdb.org/t/p/w185/person.jpg"},
		// empty path
		{name: "empty path", path: "", variant: "card", wantURL: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Each sub-test gets its own mock server to avoid shared-state issues
			// with the client's sync.Once configuration cache.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/configuration" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"images": map[string]any{
							"secure_base_url": "https://image.tmdb.org/t/p/",
						},
					})
					return
				}
				t.Errorf("unexpected path: %s", r.URL.Path)
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)

			client := provider.NewClient(1000)
			client.SetBaseURL(server.URL)
			p := provider.NewProviderWithClient(client)

			rs := &runtimeServer{provider: p}
			ms := &metadataServer{runtime: rs}

			resp, err := ms.ResolveImageURL(context.Background(), &pluginv1.ResolveImageURLRequest{
				Path:    tc.path,
				Variant: tc.variant,
			})
			if err != nil {
				t.Fatalf("ResolveImageURL() error = %v", err)
			}
			if resp.GetUrl() != tc.wantURL {
				t.Fatalf("URL = %q, want %q", resp.GetUrl(), tc.wantURL)
			}
		})
	}
}

func TestResolveImageURL_RetriesConfigurationAfterCanceledContext(t *testing.T) {
	t.Parallel()

	configCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/configuration" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		configCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"images": map[string]any{
				"secure_base_url": "https://image.tmdb.org/t/p/",
			},
		})
	}))
	t.Cleanup(server.Close)

	client := provider.NewClient(1000)
	client.SetBaseURL(server.URL)

	ms := &metadataServer{
		runtime: &runtimeServer{
			provider: provider.NewProviderWithClient(client),
		},
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ms.ResolveImageURL(canceledCtx, &pluginv1.ResolveImageURLRequest{
		Path:    "tmdb://poster/poster.jpg",
		Variant: "featured",
	}); err == nil {
		t.Fatal("ResolveImageURL() with canceled context succeeded, want error")
	}

	resp, err := ms.ResolveImageURL(context.Background(), &pluginv1.ResolveImageURLRequest{
		Path:    "tmdb://poster/poster.jpg",
		Variant: "featured",
	})
	if err != nil {
		t.Fatalf("ResolveImageURL() after canceled context error = %v", err)
	}
	if got, want := resp.GetUrl(), "https://image.tmdb.org/t/p/w500/poster.jpg"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if configCalls != 1 {
		t.Fatalf("configuration calls = %d, want 1", configCalls)
	}
}

func TestMetadataServerGetMetadata_IncludesReleaseDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": "https://image.tmdb.org/t/p/",
				},
			})
		case "/movie/123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                123,
				"title":             "Example Movie",
				"original_title":    "Example Movie",
				"overview":          "Overview",
				"tagline":           "Tagline",
				"release_date":      "2024-01-02",
				"runtime":           120,
				"vote_average":      7.2,
				"original_language": "en",
				"genres":            []map[string]any{{"id": 1, "name": "Drama"}},
				"production_companies": []map[string]any{
					{"id": 2, "name": "Studio"},
				},
				"origin_country": []string{"US"},
				"external_ids": map[string]any{
					"imdb_id": "tt1234567",
				},
				"credits": map[string]any{
					"cast": []any{},
					"crew": []any{},
				},
				"release_dates": map[string]any{
					"results": []any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := provider.NewClient(1000)
	client.SetBaseURL(server.URL)

	ms := &metadataServer{
		runtime: &runtimeServer{
			provider: provider.NewProviderWithClient(client),
		},
	}

	resp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: "123",
		ItemType:   "movie",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}

	if got := metadataItemReleaseDate(t, resp.GetItem()); got != "2024-01-02" {
		t.Fatalf("release_date = %q, want 2024-01-02", got)
	}
}

func TestMetadataServerGetMetadata_IncludesVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": map[string]any{
					"secure_base_url": "https://image.tmdb.org/t/p/",
				},
			})
		case "/movie/123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    123,
				"title": "Example Movie",
				"videos": map[string]any{
					"results": []map[string]any{
						{
							"id":           "video-1",
							"iso_639_1":    "en",
							"key":          "dQw4w9WgXcQ",
							"name":         "Official Trailer",
							"site":         "YouTube",
							"size":         1080,
							"type":         "Trailer",
							"official":     true,
							"published_at": "2024-01-15T00:00:00.000Z",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := provider.NewClient(1000)
	client.SetBaseURL(server.URL)

	ms := &metadataServer{
		runtime: &runtimeServer{
			provider: provider.NewProviderWithClient(client),
		},
	}

	resp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: "123",
		ItemType:   "movie",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}

	videos := resp.GetItem().GetVideos()
	if len(videos) != 1 {
		t.Fatalf("len(videos) = %d, want 1", len(videos))
	}

	want := &pluginv1.VideoRecord{
		ProviderKey: "video-1",
		Kind:        "trailer",
		Site:        "youtube",
		SiteKey:     "dQw4w9WgXcQ",
		Name:        "Official Trailer",
		Language:    "en",
		IsOfficial:  true,
		SizeHint:    1080,
		PublishedAt: "2024-01-15T00:00:00.000Z",
	}
	if !proto.Equal(videos[0], want) {
		t.Fatalf("videos[0] = %+v, want %+v", videos[0], want)
	}
}

func TestMetadataRequestFromProto_CarriesContextFields(t *testing.T) {
	req := &pluginv1.GetMetadataRequest{
		ProviderId: "123",
		ItemType:   "movie",
		ProviderIds: mustStruct(t, map[string]any{
			"imdb": "tt1234567",
		}),
		Language: "fr",
		FilePath: "/media/movies/example.mkv",
	}

	got := metadataRequestFromProto(req, "tmdb")

	if got.ContentType != "movie" {
		t.Fatalf("ContentType = %q, want movie", got.ContentType)
	}
	if got.Language != "fr" {
		t.Fatalf("Language = %q, want fr", got.Language)
	}
	if got.FilePath != "/media/movies/example.mkv" {
		t.Fatalf("FilePath = %q, want /media/movies/example.mkv", got.FilePath)
	}

	wantIDs := map[string]string{
		"tmdb": "123",
		"imdb": "tt1234567",
	}
	if !reflect.DeepEqual(got.ProviderIDs, wantIDs) {
		t.Fatalf("ProviderIDs = %#v, want %#v", got.ProviderIDs, wantIDs)
	}
}

func TestAssetRequestsFromProto_CarryProviderContext(t *testing.T) {
	imageReq := imageRequestFromProto(&pluginv1.GetImagesRequest{
		ProviderId: "123",
		ItemType:   "movie",
		ProviderIds: mustStruct(t, map[string]any{
			"imdb": "tt1234567",
		}),
		Language: "es",
	}, "tmdb")
	if imageReq.ContentType != "movie" {
		t.Fatalf("image ContentType = %q, want movie", imageReq.ContentType)
	}
	if imageReq.Language != "es" {
		t.Fatalf("image Language = %q, want es", imageReq.Language)
	}
	if !reflect.DeepEqual(imageReq.ProviderIDs, map[string]string{
		"tmdb": "123",
		"imdb": "tt1234567",
	}) {
		t.Fatalf("image ProviderIDs = %#v", imageReq.ProviderIDs)
	}

	seasonsReq := seasonsRequestFromProto(&pluginv1.GetSeasonsRequest{
		SeriesProviderId: "series-1",
		ProviderIds: mustStruct(t, map[string]any{
			"tvdb": "81189",
		}),
	}, "tmdb")
	if seasonsReq.ContentType != "series" {
		t.Fatalf("seasons ContentType = %q, want series", seasonsReq.ContentType)
	}
	if !reflect.DeepEqual(seasonsReq.ProviderIDs, map[string]string{
		"tmdb": "series-1",
		"tvdb": "81189",
	}) {
		t.Fatalf("seasons ProviderIDs = %#v", seasonsReq.ProviderIDs)
	}

	episodesReq := episodesRequestFromProto(&pluginv1.GetEpisodesRequest{
		SeriesProviderId: "series-1",
		SeasonNumber:     2,
		ProviderIds: mustStruct(t, map[string]any{
			"tvdb": "81189",
		}),
	}, "tmdb")
	if episodesReq.SeasonNumber != 2 {
		t.Fatalf("SeasonNumber = %d, want 2", episodesReq.SeasonNumber)
	}
	if !reflect.DeepEqual(episodesReq.ProviderIDs, map[string]string{
		"tmdb": "series-1",
		"tvdb": "81189",
	}) {
		t.Fatalf("episodes ProviderIDs = %#v", episodesReq.ProviderIDs)
	}
}

func TestMetadataServerGetPersonDetail_CanonicalizesProfilePath(t *testing.T) {
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
				"profile_path":   "/brad.jpg",
				"external_ids": map[string]any{
					"imdb_id": "nm0000093",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := provider.NewClient(1000)
	client.SetBaseURL(server.URL)

	ms := &metadataServer{
		runtime: &runtimeServer{
			provider: provider.NewProviderWithClient(client),
		},
	}

	resp, err := ms.GetPersonDetail(context.Background(), &pluginv1.GetPersonDetailRequest{
		ProviderIds: mustStruct(t, map[string]any{
			"tmdb": "287",
		}),
	})
	if err != nil {
		t.Fatalf("GetPersonDetail() error = %v", err)
	}
	if resp.GetPerson() == nil {
		t.Fatal("expected person detail record")
	}
	if resp.GetPerson().GetPhotoPath() != "tmdb://profile/brad.jpg" {
		t.Fatalf("PhotoPath = %q, want tmdb://profile/brad.jpg", resp.GetPerson().GetPhotoPath())
	}
	if resp.GetPerson().GetProviderIds().AsMap()["imdb"] != "nm0000093" {
		t.Fatalf("provider_ids[imdb] = %v, want nm0000093", resp.GetPerson().GetProviderIds().AsMap()["imdb"])
	}
}

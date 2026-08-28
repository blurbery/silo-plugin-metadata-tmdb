package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/Silo-Server/silo-plugin-tmdb/metadata"
	"github.com/Silo-Server/silo-plugin-tmdb/models"
	"github.com/Silo-Server/silo-plugin-tmdb/provider"
)

// version is set at build time via -ldflags "-X main.version=...".
var version string

type runtimeServer struct {
	pluginv1.UnimplementedRuntimeServer

	manifest *pluginv1.PluginManifest
	provider *provider.Provider
}

type metadataServer struct {
	pluginv1.UnimplementedMetadataProviderServer
	pluginv1.UnimplementedImageResolverServer
	runtime *runtimeServer
}

//go:embed manifest.json
var manifestJSON []byte

func (s *runtimeServer) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *runtimeServer) Configure(_ context.Context, _ *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *runtimeServer) providerForRequest() (*provider.Provider, error) {
	return s.provider, nil
}

func (s *metadataServer) Search(ctx context.Context, req *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.Search(ctx, metadata.SearchQuery{
		Title:       req.GetQuery(),
		Year:        int(req.GetYear()),
		ContentType: req.GetItemType(),
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
		Language:    req.GetLanguage(),
	})
	if err != nil {
		return nil, err
	}

	response := &pluginv1.SearchMetadataResponse{
		Results: make([]*pluginv1.ProviderSearchResult, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(result.ProviderIDs)
		if err != nil {
			return nil, err
		}
		response.Results = append(response.Results, &pluginv1.ProviderSearchResult{
			ProviderId:       result.ProviderIDs["tmdb"],
			ItemType:         req.GetItemType(),
			Title:            result.Name,
			Year:             int32(result.Year),
			Overview:         result.Overview,
			ProviderIds:      providerIDs,
			ImageUrl:         tmdbCanonicalPath("poster", result.ImageURL),
			OriginalTitle:    result.OriginalTitle,
			TitleAliases:     aliasesToProto(result.TitleAliases),
			TitleLanguage:    result.TitleLanguage,
			TitleIsFallback:  result.TitleIsFallback,
			OriginalLanguage: result.OriginalLanguage,
		})
	}
	return response, nil
}

func (s *metadataServer) GetMetadata(ctx context.Context, req *pluginv1.GetMetadataRequest) (*pluginv1.GetMetadataResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	result, err := p.GetMetadata(ctx, metadataRequestFromProto(req, "tmdb"))
	if err != nil || result == nil {
		return nil, err
	}

	item, err := metadataItemFromResult(result, req.GetItemType())
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetMetadataResponse{Item: item}, nil
}

func (s *metadataServer) GetPersonDetail(ctx context.Context, req *pluginv1.GetPersonDetailRequest) (*pluginv1.GetPersonDetailResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	result, err := p.GetPersonDetail(ctx, personDetailRequestFromProto(req))
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &pluginv1.GetPersonDetailResponse{}, nil
	}

	person, err := personDetailRecordFromResult(result)
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetPersonDetailResponse{Person: person}, nil
}

func (s *metadataServer) GetSeasons(ctx context.Context, req *pluginv1.GetSeasonsRequest) (*pluginv1.GetSeasonsResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.GetSeasons(ctx, seasonsRequestFromProto(req, "tmdb"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetSeasonsResponse{
		Seasons: make([]*pluginv1.SeasonRecord, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(map[string]string{"tmdb": result.ContentID})
		if err != nil {
			return nil, err
		}
		response.Seasons = append(response.Seasons, &pluginv1.SeasonRecord{
			ProviderId:   result.ContentID,
			ProviderIds:  providerIDs,
			SeasonNumber: int32(result.SeasonNumber),
			Title:        result.Title,
			Overview:     result.Overview,
			AirDate:      result.AirDate,
			PosterPath:   tmdbCanonicalPath("poster", result.PosterPath),
		})
	}
	return response, nil
}

func (s *metadataServer) GetEpisodes(ctx context.Context, req *pluginv1.GetEpisodesRequest) (*pluginv1.GetEpisodesResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.GetEpisodes(ctx, episodesRequestFromProto(req, "tmdb"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetEpisodesResponse{
		Episodes: make([]*pluginv1.EpisodeRecord, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(result.ProviderIDs)
		if err != nil {
			return nil, err
		}
		response.Episodes = append(response.Episodes, &pluginv1.EpisodeRecord{
			ProviderId:    result.ContentID,
			SeasonNumber:  int32(result.SeasonNumber),
			EpisodeNumber: int32(result.EpisodeNumber),
			Title:         result.Title,
			Overview:      result.Overview,
			AirDate:       result.AirDate,
			Runtime:       int32(result.Runtime),
			StillPath:     tmdbCanonicalPath("still", result.StillPath),
			ProviderIds:   providerIDs,
			Ratings:       ratingsStruct(result.Ratings),
		})
	}
	return response, nil
}

func (s *metadataServer) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	images, err := p.GetImages(ctx, imageRequestFromProto(req, "tmdb"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetImagesResponse{}
	for _, img := range images {
		kind := ""
		switch img.Type {
		case metadata.ImagePoster:
			kind = "poster"
		case metadata.ImageBackdrop:
			kind = "backdrop"
		case metadata.ImageLogo:
			kind = "logo"
		}
		record := &pluginv1.ImageRecord{
			Kind:     kind,
			Url:      tmdbCanonicalPath(kind, img.URL),
			Language: img.Language,
			Width:    int32(img.Width),
			Height:   int32(img.Height),
			Metadata: imageRecordMetadata(img),
		}
		if img.SeasonNumber != nil {
			seasonNumber := int32(*img.SeasonNumber)
			record.SeasonNumber = &seasonNumber
		}
		response.Images = append(response.Images, record)
	}
	return response, nil
}

func imageRecordMetadata(img metadata.RemoteImage) *structpb.Struct {
	if img.Rating <= 0 && img.IncludesText == nil {
		return nil
	}
	fields := make(map[string]any, 2)
	if img.Rating > 0 {
		fields["rating"] = img.Rating
	}
	if img.IncludesText != nil {
		fields["includes_text"] = *img.IncludesText
	}
	return structFromMap(fields)
}

// tmdbCanonicalPath wraps a raw TMDB file path with the tmdb:// scheme and role
// prefix so that the host can resolve it later via ResolveImageURL.
// Returns empty string if path is empty.
func tmdbCanonicalPath(role, path string) string {
	if path == "" {
		return ""
	}
	return "tmdb://" + role + path
}

func tmdbVariantSize(variant, role string) string {
	switch variant {
	case "card":
		if role == "profile" {
			return "w185"
		}
		return "w300"
	case "featured":
		if role == "backdrop" {
			return "w1280"
		}
		if role == "profile" {
			return "w185"
		}
		return "w500"
	case "large":
		// Sits between "featured" and "full": ~780px posters/stills and
		// ~1280px backdrops/logos, mapped to the nearest real TMDB size.
		switch role {
		case "backdrop":
			return "w1280"
		case "profile":
			// h632 is the largest non-original profile size TMDB serves.
			return "h632"
		case "logo":
			// w500 is the largest non-original logo size TMDB serves.
			return "w500"
		case "still":
			// TMDB tops stills out at w300 below original, so anything at or
			// above the "large" target has to come from original.
			return "original"
		default: // poster and anything else poster-shaped
			return "w780"
		}
	case "full":
		if role == "poster" {
			return "w780"
		}
		return "original"
	default: // "original" or empty
		return "original"
	}
}

func resolveOneTMDBPath(p *provider.Provider, barePath, variant string) string {
	if barePath == "" {
		return ""
	}
	slashIdx := strings.Index(barePath, "/")
	if slashIdx <= 0 {
		return ""
	}
	role := barePath[:slashIdx]
	tmdbPath := barePath[slashIdx:] // includes leading slash
	size := tmdbVariantSize(variant, role)
	return p.ImageURL(tmdbPath, size)
}

func (s *metadataServer) ResolveImageURL(ctx context.Context, req *pluginv1.ResolveImageURLRequest) (*pluginv1.ResolveImageURLResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}
	if err := p.LoadConfiguration(ctx); err != nil {
		return nil, err
	}
	// Strip the tmdb:// scheme prefix before resolving.
	barePath := strings.TrimPrefix(req.GetPath(), "tmdb://")
	url := resolveOneTMDBPath(p, barePath, req.GetVariant())
	return &pluginv1.ResolveImageURLResponse{Url: url}, nil
}

func (s *metadataServer) ResolveImageURLs(ctx context.Context, req *pluginv1.ResolveImageURLsRequest) (*pluginv1.ResolveImageURLsResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}
	if err := p.LoadConfiguration(ctx); err != nil {
		return nil, err
	}
	urls := make(map[string]string, len(req.GetPaths()))
	for _, path := range req.GetPaths() {
		barePath := strings.TrimPrefix(path, "tmdb://")
		urls[path] = resolveOneTMDBPath(p, barePath, req.GetVariant())
	}
	return &pluginv1.ResolveImageURLsResponse{Urls: urls}, nil
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}

	rs := &runtimeServer{
		manifest: manifest,
		provider: provider.NewProvider(),
	}

	ms := &metadataServer{runtime: rs}
	runtime.Serve(runtime.ServeConfig{
		Servers: runtime.CapabilityServers{
			Runtime:          rs,
			MetadataProvider: ms,
			ImageResolver:    ms,
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}

	if version != "" {
		manifest.Version = version
	}

	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])

	return manifest, nil
}

func metadataItemFromResult(result *metadata.MetadataResult, itemType string) (*pluginv1.MetadataItem, error) {
	providerIDs, err := stringStruct(result.ProviderIDs)
	if err != nil {
		return nil, err
	}

	return &pluginv1.MetadataItem{
		ProviderId:           result.ProviderIDs["tmdb"],
		ItemType:             itemType,
		Title:                result.Title,
		OriginalTitle:        result.OriginalTitle,
		SortTitle:            result.SortTitle,
		Year:                 int32(result.Year),
		Overview:             result.Overview,
		Tagline:              result.Tagline,
		Runtime:              int32(result.Runtime),
		Genres:               append([]string(nil), result.Genres...),
		Studios:              append([]string(nil), result.Studios...),
		Networks:             append([]string(nil), result.Networks...),
		Countries:            append([]string(nil), result.Countries...),
		Metadata:             metadataStruct(result),
		OriginalLanguage:     result.OriginalLanguage,
		ContentRating:        result.ContentRating,
		ProviderIds:          providerIDs,
		Ratings:              ratingsStruct(result.Ratings),
		PosterPath:           tmdbCanonicalPath("poster", result.PosterPath),
		PosterThumbhash:      result.PosterThumbhash,
		BackdropPath:         tmdbCanonicalPath("backdrop", result.BackdropPath),
		BackdropThumbhash:    result.BackdropThumbhash,
		LogoPath:             tmdbCanonicalPath("logo", result.LogoPath),
		SeasonCount:          int32(result.SeasonCount),
		FirstAirDate:         result.FirstAirDate,
		LastAirDate:          result.LastAirDate,
		ReleaseDate:          result.ReleaseDate,
		Status:               result.ShowStatus,
		People:               peopleToRecords(result.People),
		TitleAliases:         aliasesToProto(result.TitleAliases),
		TitleAliasesComplete: result.TitleAliasesComplete,
		TitleLanguage:        result.TitleLanguage,
		TitleIsFallback:      result.TitleIsFallback,
		Videos:               videosToRecords(result.Videos),
	}, nil
}

func aliasesToProto(aliases []metadata.TitleAlias) []*pluginv1.TitleAlias {
	out := make([]*pluginv1.TitleAlias, 0, len(aliases))
	for _, alias := range aliases {
		if strings.TrimSpace(alias.Title) == "" {
			continue
		}
		out = append(out, &pluginv1.TitleAlias{Title: alias.Title, Language: alias.Language, Kind: alias.Kind})
	}
	return out
}

func personDetailRecordFromResult(result *metadata.PersonDetailResult) (*pluginv1.PersonDetailRecord, error) {
	providerIDs, err := stringStruct(result.ProviderIDs)
	if err != nil {
		return nil, err
	}

	return &pluginv1.PersonDetailRecord{
		Name:           result.Name,
		SortName:       result.SortName,
		Bio:            result.Bio,
		BirthDate:      result.BirthDate,
		DeathDate:      result.DeathDate,
		Birthplace:     result.Birthplace,
		Homepage:       result.Homepage,
		PhotoPath:      tmdbCanonicalPath("profile", result.PhotoPath),
		PhotoThumbhash: result.PhotoThumbhash,
		ProviderIds:    providerIDs,
	}, nil
}

func metadataRequestFromProto(req *pluginv1.GetMetadataRequest, capabilityID string) metadata.MetadataRequest {
	return metadata.MetadataRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetProviderId()),
		ContentType: req.GetItemType(),
		Language:    req.GetLanguage(),
		FilePath:    req.GetFilePath(),
	}
}

func personDetailRequestFromProto(req *pluginv1.GetPersonDetailRequest) metadata.PersonDetailRequest {
	return metadata.PersonDetailRequest{
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
		Language:    req.GetLanguage(),
	}
}

func seasonsRequestFromProto(req *pluginv1.GetSeasonsRequest, capabilityID string) metadata.SeasonsRequest {
	return metadata.SeasonsRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetSeriesProviderId()),
		ContentType: "series",
		Language:    req.GetLanguage(),
	}
}

func episodesRequestFromProto(req *pluginv1.GetEpisodesRequest, capabilityID string) metadata.EpisodesRequest {
	return metadata.EpisodesRequest{
		ProviderIDs:  providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetSeriesProviderId()),
		SeasonNumber: int(req.GetSeasonNumber()),
		Language:     req.GetLanguage(),
	}
}

func imageRequestFromProto(req *pluginv1.GetImagesRequest, capabilityID string) metadata.ImageRequest {
	result := metadata.ImageRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetProviderId()),
		ContentType: req.GetItemType(),
		Language:    req.GetLanguage(),
	}
	if req.SeasonNumber != nil {
		seasonNumber := int(req.GetSeasonNumber())
		result.SeasonNumber = &seasonNumber
	}
	return result
}

func peopleToRecords(people []models.ItemPerson) []*pluginv1.PersonRecord {
	if len(people) == 0 {
		return nil
	}

	records := make([]*pluginv1.PersonRecord, 0, len(people))
	for _, person := range people {
		records = append(records, &pluginv1.PersonRecord{
			Name:           person.Name,
			Kind:           person.Kind.String(),
			Character:      person.Character,
			SortOrder:      int32(person.SortOrder),
			TmdbId:         person.TmdbID,
			TvdbId:         person.TvdbID,
			ImdbId:         person.ImdbID,
			PlexGuid:       person.PlexGUID,
			PhotoPath:      tmdbCanonicalPath("profile", person.PhotoPath),
			PhotoThumbhash: person.PhotoThumbhash,
		})
	}
	return records
}

func videosToRecords(videos []metadata.VideoResult) []*pluginv1.VideoRecord {
	if len(videos) == 0 {
		return nil
	}

	records := make([]*pluginv1.VideoRecord, 0, len(videos))
	for _, video := range videos {
		records = append(records, &pluginv1.VideoRecord{
			ProviderKey: video.ProviderKey,
			Kind:        video.Kind,
			Site:        video.Site,
			SiteKey:     video.SiteKey,
			Name:        video.Name,
			Language:    video.Language,
			IsOfficial:  video.IsOfficial,
			SizeHint:    int32(video.SizeHint),
			PublishedAt: video.PublishedAt,
		})
	}
	return records
}

func stringMapFromStruct(value *structpb.Struct) map[string]string {
	result := make(map[string]string)
	if value == nil {
		return result
	}
	for key, raw := range value.AsMap() {
		text, ok := raw.(string)
		if ok && text != "" {
			result[key] = text
		}
	}
	return result
}

func providerIDsFromProto(value *structpb.Struct, capabilityID string, fallbackID string) map[string]string {
	result := stringMapFromStruct(value)
	if fallbackID != "" && result[capabilityID] == "" {
		result[capabilityID] = fallbackID
	}
	return result
}

func stringStruct(value map[string]string) (*structpb.Struct, error) {
	if len(value) == 0 {
		return nil, nil
	}

	converted := make(map[string]any, len(value))
	for key, entry := range value {
		if entry == "" {
			continue
		}
		converted[key] = entry
	}
	if len(converted) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(converted)
}

func structFromMap(value map[string]any) *structpb.Struct {
	if len(value) == 0 {
		return nil
	}
	result, err := structpb.NewStruct(value)
	if err != nil {
		panic(err)
	}
	return result
}

func ratingsStruct(ratings metadata.Ratings) *structpb.Struct {
	return structFromMap(ratingsMap(ratings))
}

func metadataStruct(result *metadata.MetadataResult) *structpb.Struct {
	values := make(map[string]any)
	if len(result.Keywords) > 0 {
		keywords := make([]any, 0, len(result.Keywords))
		for _, keyword := range result.Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" {
				continue
			}
			keywords = append(keywords, keyword)
		}
		if len(keywords) > 0 {
			values["keywords"] = keywords
		}
	}
	return structFromMap(values)
}

func ratingsMap(ratings metadata.Ratings) map[string]any {
	result := make(map[string]any)
	if ratings.IMDB != 0 {
		result["imdb"] = ratings.IMDB
	}
	if ratings.TMDB != 0 {
		result["tmdb"] = ratings.TMDB
	}
	if ratings.RTCritic != 0 {
		result["rt_critic"] = ratings.RTCritic
	}
	if ratings.RTAudience != 0 {
		result["rt_audience"] = ratings.RTAudience
	}
	return result
}

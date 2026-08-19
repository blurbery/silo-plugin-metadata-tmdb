package provider

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Silo-Server/silo-plugin-tmdb/metadata"
	"github.com/Silo-Server/silo-plugin-tmdb/models"
)

const defaultTMDBLanguage = "en-US"

func normalizeTMDBLanguageTag(lang string) string {
	lang = strings.TrimSpace(strings.ReplaceAll(lang, "_", "-"))
	if lang == "" {
		return ""
	}

	parts := strings.Split(lang, "-")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return ""
		}
		if i == 0 {
			parts[i] = strings.ToLower(part)
			continue
		}
		parts[i] = strings.ToUpper(part)
	}
	return strings.Join(parts, "-")
}

// tmdbLanguage converts a Silo host language into a TMDB API language
// parameter. Silo sends ISO 639-1 codes; TMDB prefers region-qualified
// English, so English is normalized to en-US while other languages pass
// through in normalized form.
func tmdbLanguage(lang string) string {
	lang = normalizeTMDBLanguageTag(lang)
	if lang == "" {
		return defaultTMDBLanguage
	}
	if lang == "en" || strings.HasPrefix(lang, "en-") {
		return defaultTMDBLanguage
	}
	return lang
}

func baseLanguage(lang string) string {
	lang = normalizeTMDBLanguageTag(lang)
	if before, _, ok := strings.Cut(lang, "-"); ok {
		return before
	}
	return strings.ToLower(lang)
}

func titleFallback(requested, originalLanguage, title, originalTitle string) bool {
	return baseLanguage(requested) != "" &&
		baseLanguage(requested) != baseLanguage(originalLanguage) &&
		strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(originalTitle))
}

func titleLanguage(requested, originalLanguage, title, originalTitle string) string {
	if titleFallback(requested, originalLanguage, title, originalTitle) {
		return baseLanguage(originalLanguage)
	}
	return baseLanguage(requested)
}

func titleAliases(localized, original, originalLanguage string) []metadata.TitleAlias {
	if strings.TrimSpace(original) == "" || strings.EqualFold(strings.TrimSpace(localized), strings.TrimSpace(original)) {
		return nil
	}
	return []metadata.TitleAlias{{
		Title: original, Language: baseLanguage(originalLanguage), Kind: "original",
	}}
}

func detailTitleAliases(localized, original, originalLanguage string, alternatives *MovieAlternativeTitles) []metadata.TitleAlias {
	aliases := titleAliases(localized, original, originalLanguage)
	if alternatives != nil {
		for _, alternative := range alternatives.Titles {
			aliases = appendAlias(aliases, alternative.Title, "", "alternate", localized, original)
		}
	}
	return aliases
}

func detailTVTitleAliases(localized, original, originalLanguage string, alternatives *TVAlternativeTitles) []metadata.TitleAlias {
	aliases := titleAliases(localized, original, originalLanguage)
	if alternatives != nil {
		for _, alternative := range alternatives.Results {
			title := alternative.Title
			if title == "" {
				title = alternative.Name
			}
			aliases = appendAlias(aliases, title, "", "alternate", localized, original)
		}
	}
	return aliases
}

func appendAlias(aliases []metadata.TitleAlias, title, language, kind string, excluded ...string) []metadata.TitleAlias {
	title = strings.TrimSpace(title)
	if title == "" {
		return aliases
	}
	for _, value := range excluded {
		if strings.EqualFold(title, strings.TrimSpace(value)) {
			return aliases
		}
	}
	for _, alias := range aliases {
		if strings.EqualFold(title, strings.TrimSpace(alias.Title)) {
			return aliases
		}
	}
	return append(aliases, metadata.TitleAlias{Title: title, Language: language, Kind: kind})
}

const maxCast = 20

// Provider implements SearchProvider, MetadataProvider, ImageProvider,
// and EpisodeProvider for the TMDB v3 API.
type Provider struct {
	client *Client
}

// NewProvider creates a TMDB provider using the built-in project API key.
func NewProvider() *Provider {
	return &Provider{client: NewClient(40)}
}

// NewProviderWithClient creates a TMDB provider with a pre-configured client.
func NewProviderWithClient(c *Client) *Provider {
	return &Provider{client: c}
}

func (p *Provider) Slug() string       { return "tmdb" }
func (p *Provider) Name() string       { return "The Movie Database" }
func (p *Provider) ForTypes() []string { return []string{"movie", "series"} }

// ---------------------------------------------------------------------------
// SearchProvider
// ---------------------------------------------------------------------------

func (p *Provider) Search(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if err := p.client.loadConfiguration(ctx); err != nil {
		return nil, fmt.Errorf("tmdb: load config: %w", err)
	}

	// Try direct TMDB ID.
	if tmdbID := query.ProviderIDs["tmdb"]; tmdbID != "" {
		id, err := strconv.Atoi(tmdbID)
		if err != nil {
			return nil, fmt.Errorf("tmdb: invalid TMDB ID %q: %w", tmdbID, err)
		}
		return p.searchByTmdbID(ctx, id, query.ContentType, tmdbLanguage(query.Language))
	}

	// Try IMDb ID.
	if imdbID := query.ProviderIDs["imdb"]; imdbID != "" {
		id, err := p.findByExternalID(ctx, imdbID, "imdb_id", query.ContentType)
		if err != nil || id == 0 {
			return nil, err
		}
		return p.searchByTmdbID(ctx, id, query.ContentType, tmdbLanguage(query.Language))
	}

	// Try TVDB ID.
	if tvdbID := query.ProviderIDs["tvdb"]; tvdbID != "" {
		id, err := p.findByExternalID(ctx, tvdbID, "tvdb_id", query.ContentType)
		if err != nil || id == 0 {
			return nil, err
		}
		return p.searchByTmdbID(ctx, id, query.ContentType, tmdbLanguage(query.Language))
	}

	// Title search.
	if query.Title != "" {
		return p.searchByTitle(ctx, query)
	}

	return nil, nil
}

func (p *Provider) searchByTmdbID(ctx context.Context, id int, contentType, language string) ([]metadata.SearchResult, error) {
	switch contentType {
	case "movie":
		movie, err := p.client.GetMovie(ctx, id, language)
		if err != nil {
			return nil, err
		}
		ids := map[string]string{"tmdb": strconv.Itoa(movie.ID)}
		if movie.ExternalIDs != nil {
			if movie.ExternalIDs.IMDbID != "" {
				ids["imdb"] = movie.ExternalIDs.IMDbID
			}
			if movie.ExternalIDs.TVDBID != 0 {
				ids["tvdb"] = strconv.Itoa(movie.ExternalIDs.TVDBID)
			}
		}
		return []metadata.SearchResult{{
			Name:             movie.Title,
			OriginalTitle:    movie.OriginalTitle,
			TitleAliases:     titleAliases(movie.Title, movie.OriginalTitle, movie.OriginalLanguage),
			TitleLanguage:    titleLanguage(language, movie.OriginalLanguage, movie.Title, movie.OriginalTitle),
			TitleIsFallback:  titleFallback(language, movie.OriginalLanguage, movie.Title, movie.OriginalTitle),
			OriginalLanguage: metadata.NormalizeOriginalLanguage(movie.OriginalLanguage),
			Year:             extractYear(movie.ReleaseDate),
			ProviderIDs:      ids,
			ImageURL:         movie.PosterPath,
			Overview:         movie.Overview,
			Provider:         p.Slug(),
		}}, nil
	case "series":
		tv, err := p.client.GetTV(ctx, id, language)
		if err != nil {
			return nil, err
		}
		ids := map[string]string{"tmdb": strconv.Itoa(tv.ID)}
		if tv.ExternalIDs != nil {
			if tv.ExternalIDs.IMDbID != "" {
				ids["imdb"] = tv.ExternalIDs.IMDbID
			}
			if tv.ExternalIDs.TVDBID != 0 {
				ids["tvdb"] = strconv.Itoa(tv.ExternalIDs.TVDBID)
			}
		}
		return []metadata.SearchResult{{
			Name:             tv.Name,
			OriginalTitle:    tv.OriginalName,
			TitleAliases:     titleAliases(tv.Name, tv.OriginalName, tv.OriginalLanguage),
			TitleLanguage:    titleLanguage(language, tv.OriginalLanguage, tv.Name, tv.OriginalName),
			TitleIsFallback:  titleFallback(language, tv.OriginalLanguage, tv.Name, tv.OriginalName),
			OriginalLanguage: metadata.NormalizeOriginalLanguage(tv.OriginalLanguage),
			Year:             extractYear(tv.FirstAirDate),
			ProviderIDs:      ids,
			ImageURL:         tv.PosterPath,
			Overview:         tv.Overview,
			Provider:         p.Slug(),
		}}, nil
	}
	return nil, nil
}

func (p *Provider) searchByTitle(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	switch query.ContentType {
	case "movie":
		language := tmdbLanguage(query.Language)
		results, err := p.client.SearchMovie(ctx, query.Title, query.Year, language)
		if err != nil {
			return nil, err
		}
		var out []metadata.SearchResult
		for _, r := range results {
			out = append(out, metadata.SearchResult{
				Name:             r.Title,
				OriginalTitle:    r.OriginalTitle,
				TitleAliases:     titleAliases(r.Title, r.OriginalTitle, r.OriginalLanguage),
				TitleLanguage:    titleLanguage(language, r.OriginalLanguage, r.Title, r.OriginalTitle),
				TitleIsFallback:  titleFallback(language, r.OriginalLanguage, r.Title, r.OriginalTitle),
				OriginalLanguage: metadata.NormalizeOriginalLanguage(r.OriginalLanguage),
				Year:             extractYear(r.ReleaseDate),
				ProviderIDs:      map[string]string{"tmdb": strconv.Itoa(r.ID)},
				ImageURL:         r.PosterPath,
				Overview:         r.Overview,
				Provider:         p.Slug(),
			})
		}
		return out, nil
	case "series":
		language := tmdbLanguage(query.Language)
		results, err := p.client.SearchTV(ctx, query.Title, query.Year, language)
		if err != nil {
			return nil, err
		}
		var out []metadata.SearchResult
		for _, r := range results {
			out = append(out, metadata.SearchResult{
				Name:             r.Name,
				OriginalTitle:    r.OriginalName,
				TitleAliases:     titleAliases(r.Name, r.OriginalName, r.OriginalLanguage),
				TitleLanguage:    titleLanguage(language, r.OriginalLanguage, r.Name, r.OriginalName),
				TitleIsFallback:  titleFallback(language, r.OriginalLanguage, r.Name, r.OriginalName),
				OriginalLanguage: metadata.NormalizeOriginalLanguage(r.OriginalLanguage),
				Year:             extractYear(r.FirstAirDate),
				ProviderIDs:      map[string]string{"tmdb": strconv.Itoa(r.ID)},
				ImageURL:         r.PosterPath,
				Overview:         r.Overview,
				Provider:         p.Slug(),
			})
		}
		return out, nil
	}
	return nil, nil
}

func (p *Provider) findByExternalID(ctx context.Context, externalID, source, mediaType string) (int, error) {
	resp, err := p.client.FindByExternalID(ctx, externalID, source)
	if err != nil {
		return 0, err
	}
	switch mediaType {
	case "movie":
		if len(resp.MovieResults) > 0 {
			return resp.MovieResults[0].ID, nil
		}
	case "series":
		if len(resp.TVResults) > 0 {
			return resp.TVResults[0].ID, nil
		}
	}
	return 0, nil
}

func (p *Provider) findPersonByExternalID(ctx context.Context, externalID, source string) (int, error) {
	resp, err := p.client.FindByExternalID(ctx, externalID, source)
	if err != nil {
		return 0, err
	}
	if len(resp.PersonResults) > 0 {
		return resp.PersonResults[0].ID, nil
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// MetadataProvider
// ---------------------------------------------------------------------------

func (p *Provider) GetMetadata(ctx context.Context, req metadata.MetadataRequest) (*metadata.MetadataResult, error) {
	tmdbID := req.ProviderIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}
	if err := p.client.loadConfiguration(ctx); err != nil {
		return nil, fmt.Errorf("tmdb: load config: %w", err)
	}
	id, err := strconv.Atoi(tmdbID)
	if err != nil {
		return nil, fmt.Errorf("tmdb: invalid TMDB ID %q: %w", tmdbID, err)
	}
	lang := tmdbLanguage(req.Language)
	switch req.ContentType {
	case "movie":
		return p.getMovieMetadata(ctx, id, lang)
	case "series":
		return p.getTVMetadata(ctx, id, lang)
	}
	return nil, nil
}

func (p *Provider) GetPersonDetail(ctx context.Context, req metadata.PersonDetailRequest) (*metadata.PersonDetailResult, error) {
	personID := req.ProviderIDs["tmdb"]
	switch {
	case personID != "":
	case req.ProviderIDs["imdb"] != "":
		id, err := p.findPersonByExternalID(ctx, req.ProviderIDs["imdb"], "imdb_id")
		if err != nil || id == 0 {
			return nil, err
		}
		personID = strconv.Itoa(id)
	default:
		return nil, nil
	}

	id, err := strconv.Atoi(personID)
	if err != nil {
		return nil, fmt.Errorf("tmdb: invalid TMDB person ID %q: %w", personID, err)
	}

	person, err := p.client.GetPerson(ctx, id, tmdbLanguage(req.Language))
	if err != nil {
		return nil, err
	}

	result := &metadata.PersonDetailResult{
		Name:        person.Name,
		Bio:         person.Biography,
		BirthDate:   person.Birthday,
		DeathDate:   person.Deathday,
		Birthplace:  person.PlaceOfBirth,
		Homepage:    person.Homepage,
		PhotoPath:   person.ProfilePath,
		ProviderIDs: map[string]string{"tmdb": strconv.Itoa(person.ID)},
	}
	if person.ExternalIDs != nil && person.ExternalIDs.IMDbID != "" {
		result.ProviderIDs["imdb"] = person.ExternalIDs.IMDbID
	}
	return result, nil
}

func (p *Provider) getMovieMetadata(ctx context.Context, id int, lang string) (*metadata.MetadataResult, error) {
	movie, err := p.client.GetMovie(ctx, id, lang)
	if err != nil {
		return nil, err
	}

	result := &metadata.MetadataResult{
		HasMetadata:          true,
		Title:                movie.Title,
		OriginalTitle:        movie.OriginalTitle,
		OriginalLanguage:     metadata.NormalizeOriginalLanguage(movie.OriginalLanguage),
		TitleAliases:         detailTitleAliases(movie.Title, movie.OriginalTitle, movie.OriginalLanguage, movie.AlternativeTitles),
		TitleAliasesComplete: true,
		TitleLanguage:        titleLanguage(lang, movie.OriginalLanguage, movie.Title, movie.OriginalTitle),
		TitleIsFallback:      titleFallback(lang, movie.OriginalLanguage, movie.Title, movie.OriginalTitle),
		Overview:             movie.Overview,
		Tagline:              movie.Tagline,
		Runtime:              movie.Runtime,
		Year:                 extractYear(movie.ReleaseDate),
		ReleaseDate:          movie.ReleaseDate,
		ContentRating:        movieContentRating(movie.ReleaseDates),
		ProviderIDs:          map[string]string{"tmdb": strconv.Itoa(movie.ID)},
	}

	if movie.ExternalIDs != nil {
		if movie.ExternalIDs.IMDbID != "" {
			result.ProviderIDs["imdb"] = movie.ExternalIDs.IMDbID
		}
		if movie.ExternalIDs.TVDBID != 0 {
			result.ProviderIDs["tvdb"] = strconv.Itoa(movie.ExternalIDs.TVDBID)
		}
	}
	if result.ProviderIDs["imdb"] == "" && movie.IMDbID != "" {
		result.ProviderIDs["imdb"] = movie.IMDbID
	}

	if movie.VoteAverage > 0 {
		result.Ratings.TMDB = movie.VoteAverage
	}

	for _, g := range movie.Genres {
		result.Genres = append(result.Genres, g.Name)
	}
	if movie.Keywords != nil {
		result.Keywords = keywordNames(movie.Keywords.Keywords)
	}
	for _, c := range movie.ProductionCompanies {
		result.Studios = append(result.Studios, c.Name)
	}
	result.Countries = movie.OriginCountry

	// Carry the top-level poster/backdrop/logo paths so they're available even
	// when the images sub-response is empty (common for obscure or new titles).
	result.PosterPath = movie.PosterPath
	result.BackdropPath = movie.BackdropPath

	result.People = convertPeople(movie.Credits)
	result.Videos = convertVideos(movie.Videos)

	return result, nil
}

func (p *Provider) getTVMetadata(ctx context.Context, id int, lang string) (*metadata.MetadataResult, error) {
	tv, err := p.client.GetTV(ctx, id, lang)
	if err != nil {
		return nil, err
	}

	result := &metadata.MetadataResult{
		HasMetadata:          true,
		Title:                tv.Name,
		OriginalTitle:        tv.OriginalName,
		OriginalLanguage:     metadata.NormalizeOriginalLanguage(tv.OriginalLanguage),
		TitleAliases:         detailTVTitleAliases(tv.Name, tv.OriginalName, tv.OriginalLanguage, tv.AlternativeTitles),
		TitleAliasesComplete: true,
		TitleLanguage:        titleLanguage(lang, tv.OriginalLanguage, tv.Name, tv.OriginalName),
		TitleIsFallback:      titleFallback(lang, tv.OriginalLanguage, tv.Name, tv.OriginalName),
		Overview:             tv.Overview,
		Tagline:              tv.Tagline,
		Year:                 extractYear(tv.FirstAirDate),
		ContentRating:        tvContentRating(tv.ContentRatings),
		SeasonCount:          tv.NumberOfSeasons,
		FirstAirDate:         tv.FirstAirDate,
		LastAirDate:          tv.LastAirDate,
		ShowStatus:           tv.Status,
		ProviderIDs:          map[string]string{"tmdb": strconv.Itoa(tv.ID)},
	}

	if tv.ExternalIDs != nil {
		if tv.ExternalIDs.IMDbID != "" {
			result.ProviderIDs["imdb"] = tv.ExternalIDs.IMDbID
		}
		if tv.ExternalIDs.TVDBID != 0 {
			result.ProviderIDs["tvdb"] = strconv.Itoa(tv.ExternalIDs.TVDBID)
		}
	}

	if tv.VoteAverage > 0 {
		result.Ratings.TMDB = tv.VoteAverage
	}

	for _, g := range tv.Genres {
		result.Genres = append(result.Genres, g.Name)
	}
	if tv.Keywords != nil {
		result.Keywords = keywordNames(tv.Keywords.Results)
	}
	for _, n := range tv.Networks {
		result.Networks = append(result.Networks, n.Name)
	}
	for _, c := range tv.ProductionCompanies {
		result.Studios = append(result.Studios, c.Name)
	}
	result.Countries = tv.OriginCountry

	// Carry the top-level poster/backdrop paths so they're available even
	// when the images sub-response is empty (common for obscure or new titles).
	result.PosterPath = tv.PosterPath
	result.BackdropPath = tv.BackdropPath

	result.People = convertPeople(tv.Credits)
	result.Videos = convertVideos(tv.Videos)

	return result, nil
}

// ---------------------------------------------------------------------------
// ImageProvider
// ---------------------------------------------------------------------------

func (p *Provider) GetImages(ctx context.Context, req metadata.ImageRequest) ([]metadata.RemoteImage, error) {
	tmdbID := req.ProviderIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}
	if err := p.client.loadConfiguration(ctx); err != nil {
		return nil, fmt.Errorf("tmdb: load config: %w", err)
	}
	id, err := strconv.Atoi(tmdbID)
	if err != nil {
		return nil, fmt.Errorf("tmdb: invalid TMDB ID %q: %w", tmdbID, err)
	}

	lang := tmdbLanguage(req.Language)
	var imgs *ImageSet
	primaryPosterPath := ""
	switch req.ContentType {
	case "movie":
		movie, err := p.client.GetMovie(ctx, id, lang)
		if err != nil {
			return nil, err
		}
		imgs = movie.Images
		primaryPosterPath = movie.PosterPath
	case "series":
		tv, err := p.client.GetTV(ctx, id, lang)
		if err != nil {
			return nil, err
		}
		imgs = tv.Images
		primaryPosterPath = tv.PosterPath
	}

	var out []metadata.RemoteImage
	if imgs != nil {
		for _, img := range imgs.Posters {
			out = append(out, metadata.RemoteImage{
				URL:          img.FilePath,
				Type:         metadata.ImagePoster,
				Language:     img.ISO639_1,
				Width:        img.Width,
				Height:       img.Height,
				Rating:       img.VoteAverage,
				IncludesText: tmdbPosterIncludesText(img.ISO639_1),
			})
		}
		for _, img := range imgs.Backdrops {
			out = append(out, metadata.RemoteImage{
				URL:      img.FilePath,
				Type:     metadata.ImageBackdrop,
				Language: img.ISO639_1,
				Width:    img.Width,
				Height:   img.Height,
				Rating:   img.VoteAverage,
			})
		}
		for _, img := range imgs.Logos {
			out = append(out, metadata.RemoteImage{
				URL:      img.FilePath,
				Type:     metadata.ImageLogo,
				Language: img.ISO639_1,
				Width:    img.Width,
				Height:   img.Height,
				Rating:   img.VoteAverage,
			})
		}
	}

	return preferPrimaryImage(out, metadata.ImagePoster, primaryPosterPath, imageLanguage(lang)), nil
}

func tmdbPosterIncludesText(language string) *bool {
	result := strings.TrimSpace(language) != ""
	return &result
}

// ---------------------------------------------------------------------------
// EpisodeProvider
// ---------------------------------------------------------------------------

func (p *Provider) GetSeasons(ctx context.Context, req metadata.SeasonsRequest) ([]metadata.SeasonResult, error) {
	tmdbID := req.ProviderIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}
	if err := p.client.loadConfiguration(ctx); err != nil {
		return nil, fmt.Errorf("tmdb: load config: %w", err)
	}
	id, err := strconv.Atoi(tmdbID)
	if err != nil {
		return nil, fmt.Errorf("tmdb: invalid TMDB ID: %w", err)
	}

	lang := tmdbLanguage(req.Language)
	tv, err := p.client.GetTV(ctx, id, lang)
	if err != nil {
		return nil, err
	}

	var seasons []metadata.SeasonResult
	for _, s := range tv.Seasons {
		seasons = append(seasons, metadata.SeasonResult{
			SeasonNumber: s.SeasonNumber,
			Title:        s.Name,
			Overview:     s.Overview,
			AirDate:      s.AirDate,
			PosterPath:   s.PosterPath,
		})
	}
	return seasons, nil
}

func (p *Provider) GetEpisodes(ctx context.Context, req metadata.EpisodesRequest) ([]metadata.EpisodeResult, error) {
	tmdbID := req.ProviderIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}
	if err := p.client.loadConfiguration(ctx); err != nil {
		return nil, fmt.Errorf("tmdb: load config: %w", err)
	}
	id, err := strconv.Atoi(tmdbID)
	if err != nil {
		return nil, fmt.Errorf("tmdb: invalid TMDB ID: %w", err)
	}

	lang := tmdbLanguage(req.Language)
	season, err := p.client.GetSeason(ctx, id, req.SeasonNumber, lang)
	if err != nil {
		return nil, err
	}

	var episodes []metadata.EpisodeResult
	for _, ep := range season.Episodes {
		epResult := metadata.EpisodeResult{
			ProviderIDs:   map[string]string{"tmdb": strconv.Itoa(ep.ID)},
			SeasonNumber:  ep.SeasonNumber,
			EpisodeNumber: ep.EpisodeNumber,
			Title:         ep.Name,
			Overview:      ep.Overview,
			Runtime:       ep.Runtime,
			AirDate:       ep.AirDate,
			StillPath:     ep.StillPath,
		}
		if ep.VoteAverage > 0 {
			epResult.Ratings.TMDB = ep.VoteAverage
		}
		episodes = append(episodes, epResult)
	}
	return episodes, nil
}

func imageLanguage(lang string) string {
	norm := strings.TrimSpace(strings.ToLower(lang))
	if norm == "" {
		return ""
	}
	if idx := strings.IndexAny(norm, "-_"); idx >= 0 {
		norm = norm[:idx]
	}
	return norm
}

func preferPrimaryImage(
	images []metadata.RemoteImage,
	imageType metadata.ImageType,
	primaryURL, language string,
) []metadata.RemoteImage {
	primaryURL = strings.TrimSpace(primaryURL)
	if primaryURL == "" {
		return images
	}

	bestRating := 0.0
	primaryIdx := -1
	for i, img := range images {
		if img.Type != imageType {
			continue
		}
		if img.Rating > bestRating {
			bestRating = img.Rating
		}
		if img.URL == primaryURL {
			primaryIdx = i
		}
	}

	if primaryIdx >= 0 {
		images[primaryIdx].Rating = bestRating + 1
		if images[primaryIdx].Language == "" && language != "" {
			images[primaryIdx].Language = language
		}
		return images
	}

	return append(images, metadata.RemoteImage{
		URL:      primaryURL,
		Type:     imageType,
		Language: language,
		Rating:   bestRating + 1,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func convertPeople(credits *Credits) []models.ItemPerson {
	if credits == nil {
		return nil
	}
	var people []models.ItemPerson

	for i, cm := range credits.Cast {
		if i >= maxCast {
			break
		}
		people = append(people, models.ItemPerson{
			Person: models.Person{
				Name:      cm.Name,
				TmdbID:    strconv.Itoa(cm.ID),
				PhotoPath: tmdbProfileImageURL(cm.ProfilePath),
			},
			Kind:      models.PersonKindActor,
			Character: cm.Character,
			SortOrder: cm.Order,
		})
	}

	for _, cm := range credits.Crew {
		switch cm.Department {
		case "Directing", "Writing", "Production":
		default:
			continue
		}
		people = append(people, models.ItemPerson{
			Person: models.Person{
				Name:      cm.Name,
				TmdbID:    strconv.Itoa(cm.ID),
				PhotoPath: tmdbProfileImageURL(cm.ProfilePath),
			},
			Kind: models.PersonKindFromJob(cm.Job),
		})
	}
	return people
}

// videoKind normalizes a TMDB video type to Silo's lowercase snake_case kind.
func videoKind(tmdbType string) string {
	switch tmdbType {
	case "Trailer":
		return "trailer"
	case "Teaser":
		return "teaser"
	case "Featurette":
		return "featurette"
	case "Clip":
		return "clip"
	case "Behind the Scenes":
		return "behind_the_scenes"
	case "Bloopers":
		return "bloopers"
	default:
		return "other"
	}
}

// videoSortRank orders official trailers first, then other trailers, then
// everything else. TMDB order is preserved within each group.
func videoSortRank(v metadata.VideoResult) int {
	switch {
	case v.Kind == "trailer" && v.IsOfficial:
		return 0
	case v.Kind == "trailer":
		return 1
	default:
		return 2
	}
}

func convertVideos(w *VideosWrapper) []metadata.VideoResult {
	if w == nil || len(w.Results) == 0 {
		return nil
	}
	videos := make([]metadata.VideoResult, 0, len(w.Results))
	for _, v := range w.Results {
		if v.Key == "" || v.Site == "" {
			continue
		}
		videos = append(videos, metadata.VideoResult{
			ProviderKey: v.ID,
			Kind:        videoKind(v.Type),
			Site:        strings.ToLower(v.Site),
			SiteKey:     v.Key,
			Name:        v.Name,
			Language:    v.ISO639_1,
			IsOfficial:  v.Official,
			SizeHint:    v.Size,
			PublishedAt: v.PublishedAt,
		})
	}
	sort.SliceStable(videos, func(i, j int) bool {
		return videoSortRank(videos[i]) < videoSortRank(videos[j])
	})
	return videos
}

func keywordNames(records []Keyword) []string {
	keywords := make([]string, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		name := strings.TrimSpace(record.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keywords = append(keywords, name)
	}
	return keywords
}

func tmdbProfileImageURL(path string) string {
	return path
}

// LoadConfiguration fetches the TMDB /configuration endpoint and caches the
// image base URL. Safe to call multiple times — only the first call hits the API.
func (p *Provider) LoadConfiguration(ctx context.Context) error {
	return p.client.loadConfiguration(ctx)
}

// ImageURL builds a full image URL from a TMDB file_path and size string.
func (p *Provider) ImageURL(path, size string) string {
	return p.client.ImageURL(path, size)
}

func extractYear(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y, err := strconv.Atoi(dateStr[:4])
	if err != nil {
		return 0
	}
	return y
}

func movieContentRating(rd *ReleaseDatesWrapper) string {
	if rd == nil {
		return ""
	}
	for _, country := range rd.Results {
		if country.ISO3166_1 != "US" {
			continue
		}
		for _, rd := range country.ReleaseDates {
			if rd.Type == 3 && rd.Certification != "" {
				return rd.Certification
			}
		}
		for _, rd := range country.ReleaseDates {
			if rd.Certification != "" {
				return rd.Certification
			}
		}
	}
	return ""
}

func tvContentRating(cr *ContentRatingsWrapper) string {
	if cr == nil {
		return ""
	}
	for _, r := range cr.Results {
		if r.ISO3166_1 == "US" {
			return r.Rating
		}
	}
	return ""
}

package models

import (
	"strings"
	"time"
)

// PersonKind identifies the role type a person has on a media item.
type PersonKind int

const (
	PersonKindActor     PersonKind = 1
	PersonKindDirector  PersonKind = 2
	PersonKindWriter    PersonKind = 3
	PersonKindProducer  PersonKind = 4
	PersonKindGuestStar PersonKind = 5
	PersonKindComposer  PersonKind = 6
	// PersonKindCreator credits a series' creators. It matches Silo's value.
	PersonKindCreator PersonKind = 9
)

// String returns the Jellyfin-compatible type string for this PersonKind.
func (k PersonKind) String() string {
	switch k {
	case PersonKindActor:
		return "Actor"
	case PersonKindDirector:
		return "Director"
	case PersonKindWriter:
		return "Writer"
	case PersonKindProducer:
		return "Producer"
	case PersonKindGuestStar:
		return "GuestStar"
	case PersonKindComposer:
		return "Composer"
	case PersonKindCreator:
		return "Creator"
	default:
		return "Unknown"
	}
}

// PersonKindFromJob maps a crew job title to a PersonKind.
func PersonKindFromJob(job string) PersonKind {
	switch strings.ToLower(strings.TrimSpace(job)) {
	case "director":
		return PersonKindDirector
	case "writer", "screenplay", "story", "novel":
		return PersonKindWriter
	case "composer", "original music composer", "music":
		return PersonKindComposer
	default:
		return PersonKindProducer
	}
}

// Person represents a deduplicated person entity.
type Person struct {
	ID             int64
	Name           string
	SortName       string
	Bio            string
	BirthDate      *time.Time
	DeathDate      *time.Time
	Birthplace     string
	Homepage       string
	PhotoPath      string
	PhotoThumbhash string
	TmdbID         string
	ImdbID         string
	TvdbID         string
	PlexGUID       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ItemPerson represents a person's credit on a specific media item.
type ItemPerson struct {
	Person
	Kind      PersonKind
	Character string
	SortOrder int
}

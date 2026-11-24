package bookcontent

import (
	"context"
	"errors"

	"yomifolio/internal/config"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
)

// ComicInfoClue distinguishes absent, corrupt, unavailable and oversized evidence.
// Only modeled identity fields are exposed, never the raw archive or host path.
type ComicInfoClue struct {
	Status    string `json:"status"`
	Title     string `json:"title,omitempty"`
	Series    string `json:"series,omitempty"`
	Number    string `json:"number,omitempty"`
	Volume    string `json:"volume,omitempty"`
	Writer    string `json:"writer,omitempty"`
	Penciller string `json:"penciller,omitempty"`
	Publisher string `json:"publisher,omitempty"`
}

// ComicInfo resolves current book identity under library admission and returns no retained handles.
func (s *Service) ComicInfo(ctx context.Context, id, libraryID int64, access LibraryAccess) (ComicInfoClue, error) {
	for {
		if err := ctx.Err(); err != nil {
			return ComicInfoClue{}, err
		}
		version := access.LibraryAccessVersion()
		source, err := s.lookup(ctx, id)
		if err != nil || source.LibraryID != libraryID {
			return ComicInfoClue{}, ErrSourceMissing
		}
		release, err := access.AcquireLibraryRead(source.LibraryID)
		if err != nil {
			return ComicInfoClue{}, ErrBusy
		}
		if version != access.LibraryAccessVersion() {
			release()
			continue
		}
		policy := config.ResolveStoragePolicy(s.config(), source.Path)
		lease, err := storageio.Default.Acquire(ctx, storageio.Request{VolumeKey: policy.VolumeKey, Limit: policy.IOPolicy.ArchiveOpenConcurrency, Kind: storageio.WorkKindReader, PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading})
		if err != nil {
			release()
			return ComicInfoClue{}, err
		}
		clue := readComicInfo(ctx, source.Path)
		lease.Release()
		release()
		return clue, ctx.Err()
	}
}
func readComicInfo(ctx context.Context, path string) ComicInfoClue {
	arc, release, err := parser.GetArchiveFromPool(path)
	if err != nil {
		return ComicInfoClue{Status: "unavailable"}
	}
	defer release()
	raw, err := parser.ReadMetadataLimited(ctx, arc, "ComicInfo.xml", 64<<10)
	if errors.Is(err, parser.ErrRarEvidenceUnsupported) {
		return ComicInfoClue{Status: "unavailable"}
	}
	if errors.Is(err, parser.ErrMetadataNotFound) {
		return ComicInfoClue{Status: "missing"}
	}
	if errors.Is(err, parser.ErrMetadataTooLarge) {
		return ComicInfoClue{Status: "too_large"}
	}
	if err != nil {
		return ComicInfoClue{Status: "unreadable"}
	}
	if len(raw) > 64<<10 {
		return ComicInfoClue{Status: "too_large"}
	}
	info, err := parser.ParseComicInfo(raw)
	if err != nil {
		return ComicInfoClue{Status: "corrupt"}
	}
	return ComicInfoClue{Status: "available", Title: info.Title, Series: info.Series, Number: info.Number, Volume: info.Volume, Writer: info.Writer, Penciller: info.Penciller, Publisher: info.Publisher}
}

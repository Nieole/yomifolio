// EntryOf is how every writer names a source entry, so a conclusion, an exclusion and a later
// candidate for the same entry compare equal. Entry URLs reach the owner's page as links, which
// is why only http(s) URLs survive.

package identification_test

import (
	"testing"

	"yomifolio/internal/application/identification"
)

func TestEntryOf(t *testing.T) {
	const uuid = "0b4b1b7c-1111-2222-3333-444455556666"
	cases := []struct {
		name      string
		source    string
		id        int64
		url       string
		want      identification.Entry
		wantFound bool
	}{
		{"numeric id with url", "bangumi", 42, "https://bgm.tv/subject/42", identification.Entry{Source: "bangumi", ID: "42", URL: "https://bgm.tv/subject/42"}, true},
		{"numeric id builds url", "anilist", 7, "", identification.Entry{Source: "anilist", ID: "7", URL: "https://anilist.co/manga/7"}, true},
		{"id read from url", "myanimelist", 0, "https://myanimelist.net/manga/13/One_Piece", identification.Entry{Source: "myanimelist", ID: "13", URL: "https://myanimelist.net/manga/13/One_Piece"}, true},
		{"mangadex uuid", "mangadex", 0, "https://mangadex.org/title/" + uuid, identification.Entry{Source: "mangadex", ID: uuid, URL: "https://mangadex.org/title/" + uuid}, true},
		{"comic vine keeps its url", "comicvine", 0, "https://comicvine.gamespot.com/saga/4050-49901/", identification.Entry{Source: "comicvine", ID: "49901", URL: "https://comicvine.gamespot.com/saga/4050-49901/"}, true},
		{"comic vine without url", "comicvine", 49901, "", identification.Entry{Source: "comicvine", ID: "49901"}, true},
		{"script url is dropped", "bangumi", 5, "javascript:alert(1)", identification.Entry{Source: "bangumi", ID: "5", URL: "https://bgm.tv/subject/5"}, true},
		{"no identity", "bangumi", 0, "", identification.Entry{}, false},
		{"unknown source", "llm", 3, "https://bgm.tv/subject/3", identification.Entry{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := identification.EntryOf(tc.source, tc.id, tc.url)
			if found != tc.wantFound || got != tc.want {
				t.Fatalf("EntryOf(%q, %d, %q) = %+v, %t want %+v, %t", tc.source, tc.id, tc.url, got, found, tc.want, tc.wantFound)
			}
		})
	}
}

func TestSourceKey(t *testing.T) {
	for name, want := range map[string]string{
		"Bangumi": "bangumi", "bgm": "bangumi", "AniList": "anilist", "MangaDex": "mangadex",
		"MyAnimeList": "myanimelist", "mal": "myanimelist", "Comic Vine": "comicvine", "comic-vine": "comicvine",
	} {
		if got, ok := identification.SourceKey(name); !ok || got != want {
			t.Errorf("SourceKey(%q) = %q, %t want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "llm", "OpenAI", "delegated", "manual"} {
		if got, ok := identification.SourceKey(name); ok {
			t.Errorf("SourceKey(%q) = %q, want no source", name, got)
		}
	}
}

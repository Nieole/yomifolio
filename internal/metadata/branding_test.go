package metadata

import (
	"context"
	"net/http"
	"testing"
)

func TestProviderYomifolioUserAgent(t *testing.T) {
	for _, name := range []string{"anilist", "bangumi", "comicvine", "mangadex"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if got, want := req.Header.Get("User-Agent"), "Yomifolio/1.0 (https://github.com/Nieole/yomifolio)"; got != want {
					t.Errorf("User-Agent = %q, want %q", got, want)
				}
				return metaJSONResponse(req, `{}`), nil
			})}
			switch name {
			case "anilist":
				p := NewAniListProvider()
				p.httpClient = client
				_, _, _ = p.SearchMetadata(context.Background(), "test", 10, 0)
			case "bangumi":
				p := NewBangumiProvider()
				p.httpClient = client
				_, _, _ = p.SearchMetadata(context.Background(), "test", 10, 0)
			case "comicvine":
				p := NewComicVineProvider("test-key")
				p.httpClient = client
				_, _, _ = p.SearchMetadata(context.Background(), "test", 10, 0)
			case "mangadex":
				p := NewMangaDexProvider()
				p.httpClient = client
				_, _, _ = p.SearchMetadata(context.Background(), "test", 10, 0)
			}
			if calls == 0 {
				t.Fatal("provider did not send a request")
			}
		})
	}
}

package api

import "yomifolio/internal/bookcontent"

type imageMemoryCache = bookcontent.MemoryCache

const defaultImageCacheBytes = bookcontent.DefaultImageCacheBytes

func newImageMemoryCache(maxBytes int64) *imageMemoryCache {
	return bookcontent.NewMemoryCache(maxBytes)
}

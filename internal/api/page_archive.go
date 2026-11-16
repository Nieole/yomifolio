package api

import (
	"yomifolio/internal/bookcontent"
	"yomifolio/internal/database"
)

type bookPageSource = bookcontent.Source

func bookPageSourceFromBook(book database.Book) bookPageSource {
	return bookcontent.Source{ID: book.ID, LibraryID: book.LibraryID, Path: book.Path, FileModifiedAt: book.FileModifiedAt, Size: book.Size, PageCount: book.PageCount}
}

func (c *contentHTTP) purgeReadingPathCaches() { c.content.PurgeSources() }

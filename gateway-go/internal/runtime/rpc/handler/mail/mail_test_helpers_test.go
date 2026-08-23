package handlermail

import (
	"context"
	"errors"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type fakeMemoryStore struct {
	searchFn         func(ctx context.Context, q string, limit int) ([]wiki.SearchResult, error)
	querySearchFn    func(ctx context.Context, q string, limit int, options wiki.QueryOptions) (wiki.SearchReport, error)
	searchDiaryFn    func(ctx context.Context, q string, limit int) ([]wiki.DiaryHit, error)
	readPageFn       func(relPath string) (*wiki.Page, error)
	writePageFn      func(relPath string, page *wiki.Page) error
	deletePageFn     func(relPath string) error
	movePageFn       func(from, to string) error
	statsFn          func() wiki.StoreStats
	listPagesFn      func(category string) ([]string, error)
	diaryRecentFn    func(limit int) []wiki.DiaryHit
	seenQueryOptions []wiki.QueryOptions
}

func (f *fakeMemoryStore) SearchWithOptions(ctx context.Context, q string, n int, options wiki.QueryOptions) (wiki.SearchReport, error) {
	f.seenQueryOptions = append(f.seenQueryOptions, options)
	if f.querySearchFn != nil {
		return f.querySearchFn(ctx, q, n, options)
	}
	results, err := f.Search(ctx, q, n)
	return wiki.SearchReport{Results: results}, err
}

func (f *fakeMemoryStore) Search(ctx context.Context, q string, n int) ([]wiki.SearchResult, error) {
	if f.searchFn == nil {
		return nil, errors.New("Search not stubbed")
	}
	return f.searchFn(ctx, q, n)
}

func (f *fakeMemoryStore) SearchDiary(ctx context.Context, q string, n int) ([]wiki.DiaryHit, error) {
	if f.searchDiaryFn == nil {
		return nil, errors.New("SearchDiary not stubbed")
	}
	return f.searchDiaryFn(ctx, q, n)
}

func (f *fakeMemoryStore) ReadPage(relPath string) (*wiki.Page, error) {
	if f.readPageFn == nil {
		return nil, errors.New("ReadPage not stubbed")
	}
	return f.readPageFn(relPath)
}

func (f *fakeMemoryStore) WritePage(relPath string, page *wiki.Page) error {
	if f.writePageFn == nil {
		return errors.New("WritePage not stubbed")
	}
	return f.writePageFn(relPath, page)
}

func (f *fakeMemoryStore) DeletePage(relPath string) error {
	if f.deletePageFn == nil {
		return errors.New("DeletePage not stubbed")
	}
	return f.deletePageFn(relPath)
}

func (f *fakeMemoryStore) MovePage(from, to string) error {
	if f.movePageFn == nil {
		return errors.New("MovePage not stubbed")
	}
	return f.movePageFn(from, to)
}

func (f *fakeMemoryStore) Stats() wiki.StoreStats {
	if f.statsFn == nil {
		return wiki.StoreStats{}
	}
	return f.statsFn()
}

func (f *fakeMemoryStore) ListPages(category string) ([]string, error) {
	if f.listPagesFn == nil {
		return nil, errors.New("ListPages not stubbed")
	}
	return f.listPagesFn(category)
}

func (f *fakeMemoryStore) RecentDiaryEntries(limit int) []wiki.DiaryHit {
	if f.diaryRecentFn == nil {
		return nil
	}
	return f.diaryRecentFn(limit)
}

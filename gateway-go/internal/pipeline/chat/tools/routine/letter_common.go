package routine

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type letterCollector struct {
	index int
	load  func(context.Context) any
}

func collectLetterSections(ctx context.Context, count int, collectors []letterCollector) []any {
	if count < 0 {
		count = 0
	}
	results := make([]any, count)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, collector := range collectors {
		if collector.index < 0 || collector.index >= count || collector.load == nil {
			continue
		}
		wg.Add(1)
		go func(index int, load func(context.Context) any) {
			defer wg.Done()
			sectionCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			data := load(sectionCtx)
			mu.Lock()
			results[index] = data
			mu.Unlock()
		}(collector.index, collector.load)
	}
	wg.Wait()
	return results
}

func koreanDate(now time.Time) string {
	weekday := [...]string{"일", "월", "화", "수", "목", "금", "토"}[now.Weekday()]
	return fmt.Sprintf("%d년 %d월 %d일 %s요일", now.Year(), int(now.Month()), now.Day(), weekday)
}

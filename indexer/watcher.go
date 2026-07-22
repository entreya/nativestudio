package indexer

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	Scanner  Scanner
	Debounce time.Duration
}

func (w Watcher) Run(ctx context.Context, root string, onChange func(string), onRemove func(string)) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			rel, _ := filepath.Rel(root, path)
			if rel != "." && w.Scanner.exclusionReason(filepath.ToSlash(rel)) == "excluded_directory" {
				return filepath.SkipDir
			}
			_ = watcher.Add(path)
		}
		return nil
	})
	delay := w.Debounce
	if delay <= 0 {
		delay = 350 * time.Millisecond
	}
	timers := map[string]*time.Timer{}
	var mu sync.Mutex
	defer func() {
		mu.Lock()
		for _, timer := range timers {
			timer.Stop()
		}
		mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					_ = watcher.Add(event.Name)
					continue
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			path := event.Name
			mu.Lock()
			if existing := timers[path]; existing != nil {
				existing.Stop()
			}
			timers[path] = time.AfterFunc(delay, func() {
				mu.Lock()
				delete(timers, path)
				mu.Unlock()
				if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
					onRemove(path)
				} else {
					onChange(path)
				}
			})
			mu.Unlock()
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[indexer] file watcher error for %s: %v", root, watchErr)
		}
	}
}

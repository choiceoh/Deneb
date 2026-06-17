package lmtpd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Queue durably stores LMTP deliveries after parse and before analysis. The MTA
// can be ACKed once Enqueue succeeds; analysis workers then retry queued raw
// messages across gateway restarts.
type Queue struct {
	mu            sync.Mutex
	dir           string
	pendingDir    string
	processingDir string
	failedDir     string
}

type QueueItem struct {
	Key       string    `json:"key"`
	Raw       []byte    `json:"raw"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	Attempts  int       `json:"attempts,omitempty"`
	LastError string    `json:"lastError,omitempty"`

	path string
	name string
}

type QueueStats struct {
	Pending    int
	Processing int
	Failed     int
}

func NewQueue(dir string) (*Queue, error) {
	if dir == "" {
		return nil, fmt.Errorf("lmtp queue: empty dir")
	}
	q := &Queue{
		dir:           dir,
		pendingDir:    filepath.Join(dir, "pending"),
		processingDir: filepath.Join(dir, "processing"),
		failedDir:     filepath.Join(dir, "failed"),
	}
	for _, d := range []string{q.pendingDir, q.processingDir, q.failedDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	if err := q.recoverProcessing(); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *Queue) Dir() string { return q.dir }

func (q *Queue) Enqueue(msg *Message) (bool, error) {
	if msg == nil {
		return false, fmt.Errorf("lmtp queue: nil message")
	}
	key := msg.DedupKey
	if key == "" && msg.Detail != nil {
		key = msg.Detail.ID
	}
	if key == "" {
		return false, fmt.Errorf("lmtp queue: empty key")
	}
	if len(msg.Raw) == 0 {
		return false, fmt.Errorf("lmtp queue: empty raw message")
	}
	name := queueFileName(key)
	item := &QueueItem{
		Key:       key,
		Raw:       append([]byte(nil), msg.Raw...),
		CreatedAt: time.Now().UTC(),
		name:      name,
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.existsLocked(name) {
		return false, nil
	}
	return true, q.writeItemAtomic(filepath.Join(q.pendingDir, name), item)
}

func (q *Queue) Claim() (*QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := os.ReadDir(q.pendingDir)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		name    string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{name: e.Name(), modTime: info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].modTime.Before(candidates[j].modTime)
	})

	for _, c := range candidates {
		pending := filepath.Join(q.pendingDir, c.name)
		processing := filepath.Join(q.processingDir, c.name)
		if err := os.Rename(pending, processing); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		item, err := q.readItem(processing)
		if err != nil {
			_ = os.Rename(processing, filepath.Join(q.failedDir, c.name))
			return nil, err
		}
		item.path = processing
		item.name = c.name
		return item, nil
	}
	return nil, nil
}

func (q *Queue) Complete(item *QueueItem) error {
	if item == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return removeIfExists(q.processingPath(item))
}

func (q *Queue) Fail(item *QueueItem, cause error, maxAttempts int) error {
	if item == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	item.Attempts++
	item.UpdatedAt = time.Now().UTC()
	if cause != nil {
		item.LastError = cause.Error()
	}
	src := q.processingPath(item)
	if maxAttempts > 0 && item.Attempts >= maxAttempts {
		dst := filepath.Join(q.failedDir, q.itemName(item))
		if err := q.writeItemAtomic(dst, item); err != nil {
			return err
		}
		return removeIfExists(src)
	}
	dst := filepath.Join(q.pendingDir, q.itemName(item))
	if err := q.writeItemAtomic(dst, item); err != nil {
		return err
	}
	return removeIfExists(src)
}

func (q *Queue) Stats() QueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return QueueStats{
		Pending:    countFiles(q.pendingDir),
		Processing: countFiles(q.processingDir),
		Failed:     countFiles(q.failedDir),
	}
}

func (q *Queue) recoverProcessing() error {
	entries, err := os.ReadDir(q.processingDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(q.processingDir, e.Name())
		dst := filepath.Join(q.pendingDir, e.Name())
		if _, err := os.Stat(filepath.Join(q.failedDir, e.Name())); err == nil {
			if rmErr := os.Remove(src); rmErr != nil {
				return rmErr
			}
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			if rmErr := os.Remove(src); rmErr != nil {
				return rmErr
			}
			continue
		}
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (q *Queue) existsLocked(name string) bool {
	for _, dir := range []string{q.pendingDir, q.processingDir, q.failedDir} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func (q *Queue) readItem(path string) (*QueueItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var item QueueItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	if item.Key == "" || len(item.Raw) == 0 {
		return nil, fmt.Errorf("lmtp queue: corrupt item %s", path)
	}
	return &item, nil
}

func (q *Queue) writeItemAtomic(path string, item *QueueItem) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (q *Queue) processingPath(item *QueueItem) string {
	if item.path != "" {
		return item.path
	}
	return filepath.Join(q.processingDir, q.itemName(item))
}

func (q *Queue) itemName(item *QueueItem) string {
	if item.name != "" {
		return item.name
	}
	return queueFileName(item.Key)
}

func queueFileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".json"
}

func countFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

const (
	auditPath    = "state/audit/security.jsonl"
	maxTailBytes = 1 << 20
)

type Event struct {
	Time       time.Time `json:"time"`
	Action     string    `json:"action"`
	Outcome    string    `json:"outcome"`
	Actor      string    `json:"actor,omitempty"`
	ClientHash string    `json:"clientHash,omitempty"`
}

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Record(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.Time = time.Now().UTC()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(s.repository.Root(), filepath.FromSlash(auditPath))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (s *Service) List(limit int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	path := filepath.Join(s.repository.Root(), filepath.FromSlash(auditPath))
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := info.Size() - maxTailBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTailBytes))
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		} else {
			return []Event{}, nil
		}
	}
	events := make([]Event, 0, limit)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var event Event
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Action != "" && event.Outcome != "" {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.After(events[j].Time) })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

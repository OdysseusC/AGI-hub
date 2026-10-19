package manager

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	journalName           = "state.jsonl"
	maxJournalRecordBytes = 4 << 20
	maxJournalBytes       = 128 << 20
)

type journalRecord struct {
	Snapshot json.RawMessage `json:"snapshot"`
	Checksum string          `json:"checksum"`
}

// A kernel-owned listener gives the process lock automatic crash cleanup on all
// supported operating systems. A port collision fails closed; it is never stolen.
func acquireDirectoryLock(dir string) (net.Listener, error) {
	key := dir
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		key = strings.ToLower(key)
	}
	hash := sha256.Sum256([]byte(key))
	port := 20000 + (int(hash[0])<<8|int(hash[1]))%40000
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, fmt.Errorf("data directory is already in use or its process-lock port is occupied: %w", err)
	}
	return listener, nil
}

func openJournal(dir string) (*os.File, Snapshot, []string, error) {
	file, err := os.OpenFile(filepath.Join(dir, journalName), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, Snapshot{}, nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() > maxJournalBytes {
		file.Close()
		if err != nil {
			return nil, Snapshot{}, nil, err
		}
		return nil, Snapshot{}, nil, errors.New("journal exceeds the V0 128 MiB limit")
	}
	state := emptySnapshot()
	var order []string
	seen := make(map[string]bool)
	reader := bufio.NewReader(file)
	var validOffset int64
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				// The newline is the commit boundary. A partial final record is
				// discarded, including a complete JSON value missing its newline.
				if err = file.Truncate(validOffset); err == nil {
					err = file.Sync()
				}
				if err != nil {
					file.Close()
					return nil, Snapshot{}, nil, fmt.Errorf("repair journal tail: %w", err)
				}
			}
			break
		}
		if readErr != nil {
			file.Close()
			return nil, Snapshot{}, nil, readErr
		}
		lineNumber++
		if len(line) > maxJournalRecordBytes {
			file.Close()
			return nil, Snapshot{}, nil, fmt.Errorf("journal record %d exceeds the V0 4 MiB limit", lineNumber)
		}
		var record journalRecord
		if err = json.Unmarshal(bytes.TrimSuffix(line, []byte{'\n'}), &record); err != nil {
			file.Close()
			return nil, Snapshot{}, nil, fmt.Errorf("journal record %d is corrupt: %w", lineNumber, err)
		}
		checksum := sha256.Sum256(record.Snapshot)
		if record.Checksum != hex.EncodeToString(checksum[:]) {
			file.Close()
			return nil, Snapshot{}, nil, fmt.Errorf("journal record %d checksum mismatch", lineNumber)
		}
		var next Snapshot
		if err = json.Unmarshal(record.Snapshot, &next); err != nil || !validSnapshot(next) {
			file.Close()
			return nil, Snapshot{}, nil, fmt.Errorf("journal record %d has an invalid snapshot", lineNumber)
		}
		for _, id := range sortedMessageIDs(next.Messages) {
			if !seen[id] {
				order = append(order, id)
				seen[id] = true
			}
		}
		state = next
		validOffset += int64(len(line))
	}
	return file, state, order, nil
}

func appendSnapshot(file *os.File, state Snapshot) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	checksum := sha256.Sum256(payload)
	line, err := json.Marshal(journalRecord{Snapshot: payload, Checksum: hex.EncodeToString(checksum[:])})
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if len(line) > maxJournalRecordBytes {
		return errors.New("snapshot exceeds the V0 4 MiB journal record limit")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size()+int64(len(line)) > maxJournalBytes {
		return errors.New("journal reached the V0 128 MiB limit; history was preserved")
	}
	for len(line) > 0 {
		n, writeErr := file.Write(line)
		if writeErr != nil {
			return writeErr
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		line = line[n:]
	}
	return file.Sync()
}

func validSnapshot(s Snapshot) bool {
	return (s.Version == 1 || s.Version == 2 || s.Version == 3) && s.Endpoints != nil && s.Sessions != nil && s.Tasks != nil && s.Messages != nil && s.EventIDs != nil && s.ResourceOwners != nil
}

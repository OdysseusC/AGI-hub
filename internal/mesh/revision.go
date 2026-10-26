package mesh

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Reserving a range once per process avoids a disk write for every signed
// response. Unused numbers are skipped after restart; revisions never use UTC.
const directoryRevisionBlock uint64 = 1 << 32

// The caller owns mu, or is still constructing the unshared Service. The Hub's
// data directory has one owning runtime, like the identity and mailbox state.
func (s *Service) reserveDirectoryRevisions() error {
	path := filepath.Join(s.opts.DataDir, "directory-revision.json")
	var previous uint64
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if previous > ^uint64(0)-directoryRevisionBlock {
		return errors.New("directory revision exhausted")
	}
	limit := previous + directoryRevisionBlock
	if err := atomicJSON(path, limit); err != nil {
		return err
	}
	s.revision, s.revisionLimit = previous, limit
	return nil
}

func (s *Service) nextDirectoryRevisionLocked() (uint64, error) {
	if s.revision == s.revisionLimit {
		if err := s.reserveDirectoryRevisions(); err != nil {
			return 0, err
		}
	}
	s.revision++
	return s.revision, nil
}

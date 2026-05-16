package snapshot

import (
	"github.com/taichirain/portkey/internal/domain/credential"
)

type SnapshotCredentialFetcher struct {
	snap *ConfigSnapshot
}

func NewSnapshotCredentialFetcher(snap *ConfigSnapshot) *SnapshotCredentialFetcher {
	return &SnapshotCredentialFetcher{snap: snap}
}

func (f *SnapshotCredentialFetcher) GetByKey(key string) (*credential.Credential, error) {
	cred, ok := f.snap.GetCredentialByKey(key)
	if !ok {
		return nil, nil
	}
	return cred, nil
}

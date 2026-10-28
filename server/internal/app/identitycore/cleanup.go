package identitycore

import (
	"time"

	"github.com/QuantumNous/new-api/common"
)

const authArtifactCleanupInterval = time.Hour

// StartAuthArtifactCleanup removes expired dashboard Sessions and old
// one-time authentication flows. Only the master instance performs cleanup.
func StartAuthArtifactCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		cleanupAuthArtifacts()
		ticker := time.NewTicker(authArtifactCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupAuthArtifacts()
		}
	}()
}

func cleanupAuthArtifacts() { _ = identityRuntime().Cleanup.Execute() }

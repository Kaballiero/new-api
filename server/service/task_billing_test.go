package service

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
)

func TestMain(m *testing.M)                { taskfixture.Main(m) }
func truncate(t *testing.T)                { taskfixture.Truncate(t) }
func seedUser(t *testing.T, id, quota int) { taskfixture.SeedUser(t, id, quota) }
func seedToken(t *testing.T, id, userID int, key string, quota int) {
	taskfixture.SeedToken(t, id, userID, key, quota)
}
func seedChannel(t *testing.T, id int) { taskfixture.SeedChannel(t, id) }

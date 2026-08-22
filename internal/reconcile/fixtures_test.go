package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestGoldenDesiredSnapshotAndPlanHashes(t *testing.T) {
	for _, name := range []string{"small", "complex"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				DesiredHash string `json:"desired_hash"`
				Snapshot    struct {
					CanonicalHash string `json:"canonical_hash"`
				} `json:"snapshot"`
				Plan struct {
					Hash           string `json:"hash"`
					OperationCount int    `json:"operation_count"`
				} `json:"plan"`
			}
			expectedData, err := os.ReadFile(filepath.Join("testdata", name+".expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(expectedData, &expected); err != nil {
				t.Fatal(err)
			}
			desired, err := ParseDesired(data)
			if err != nil {
				t.Fatal(err)
			}
			desiredHash, err := DesiredHash(desired)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := NormalizeSnapshot(SnapshotInput{Guild: domain.Guild{ID: desired.GuildID, Name: "Fixture Guild"}, ApplicationID: desired.ApplicationID, CapturedAt: time.Unix(1, 0)})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "fixture-actor", CreatedAt: time.Unix(2, 0)})
			if err != nil {
				t.Fatal(err)
			}
			if desiredHash != expected.DesiredHash || snapshot.Metadata.CanonicalHash != expected.Snapshot.CanonicalHash || string(plan.Hash) != expected.Plan.Hash || len(plan.Operations) != expected.Plan.OperationCount {
				t.Fatalf("golden artifacts changed: desired=%s snapshot=%s plan=%s operations=%d", desiredHash, snapshot.Metadata.CanonicalHash, plan.Hash, len(plan.Operations))
			}
		})
	}
}

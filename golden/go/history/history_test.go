package history

import (
	"os"
	"testing"
	"time"

	assert "github.com/stretchr/testify/require"
	"go.skia.org/infra/go/eventbus"
	"go.skia.org/infra/go/testutils"

	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/golden/go/digeststore"
	"go.skia.org/infra/golden/go/mocks"
	"go.skia.org/infra/golden/go/storage"
	"go.skia.org/infra/golden/go/types"
)

const TEST_DATA_DIR = "testdata"

func BenchmarkHistory(b *testing.B) {
	assert.NoError(b, os.MkdirAll(TEST_DATA_DIR, 0755))
	defer testutils.RemoveAll(b, TEST_DATA_DIR)

	digestStore, err := digeststore.New(TEST_DATA_DIR)
	assert.NoError(b, err)

	tileBuilder := mocks.GetTileBuilderFromEnv(b)
	storages := &storage.Storage{
		MasterTileBuilder: tileBuilder,
		DigestStore:       digestStore,
		EventBus:          eventbus.New(nil),
	}

	tile := tileBuilder.GetTile()
	assert.NoError(b, Init(storages, 0))

	// Gather the runtimes of the testname/digest lookup.
	runtimes := make([]int64, 0, 1000000)
	timeIt := func(testName, digest string) (bool, error) {
		startTime := time.Now().UnixNano()
		_, found, err := digestStore.Get(testName, digest)
		runtimes = append(runtimes, (time.Now().UnixNano()-startTime)/1000)
		return found, err
	}

	b.ResetTimer()
	tileLen := tile.LastCommitIndex() + 1
	for _, trace := range tile.Traces {
		testName := trace.Params()[types.PRIMARY_KEY_FIELD]
		gTrace := trace.(*types.GoldenTrace)
		for _, digest := range gTrace.Values[:tileLen] {
			if digest != types.MISSING_DIGEST {
				found, err := timeIt(testName, digest)
				assert.NoError(b, err)
				assert.True(b, found)
			}
		}
	}

	var avg int64 = 0
	for _, val := range runtimes {
		avg += val
	}
	sklog.Infof("Average lookup time: %.3fus", float64(avg)/float64(len(runtimes)))
	sklog.Infof("Number of lookups  : %d", len(runtimes))
}

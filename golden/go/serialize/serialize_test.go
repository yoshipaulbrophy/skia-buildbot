package serialize

import (
	"bytes"
	"fmt"
	"testing"

	assert "github.com/stretchr/testify/require"

	"go.skia.org/infra/go/gcs"
	"go.skia.org/infra/go/paramtools"
	"go.skia.org/infra/go/testutils"
	"go.skia.org/infra/go/tiling"
	"go.skia.org/infra/golden/go/mocks"
	"go.skia.org/infra/golden/go/types"
)

const (
	// Directory with testdata.
	TEST_DATA_DIR = "./testdata"

	// Local file location of the test data.
	TEST_DATA_PATH = TEST_DATA_DIR + "/goldentile.json"

	// Folder in the testdata bucket. See go/testutils for details.
	TEST_DATA_STORAGE_BUCKET = "skia-infra-testdata"
	TEST_DATA_STORAGE_PATH   = "gold-testdata/goldentile.json.gz"
)

var testParamsList = []paramtools.Params{
	map[string]string{
		"name":             "foo1",
		"config":           "8888",
		types.CORPUS_FIELD: "gmx",
	},
	map[string]string{
		"name":             "foo1",
		"config":           "565",
		types.CORPUS_FIELD: "gmx",
	},
	map[string]string{
		"name":             "foo2",
		"config":           "gpu",
		types.CORPUS_FIELD: "gm",
	},
}

func TestSerializeStrings(t *testing.T) {
	testutils.SmallTest(t)
	testArr := []string{}
	for i := 0; i < 100; i++ {
		testArr = append(testArr, fmt.Sprintf("str-%4d", i))
	}

	bytesArr := stringsToBytes(testArr)
	found, err := bytesToStrings(bytesArr)
	assert.NoError(t, err)
	assert.Equal(t, testArr, found)

	var bufWriter bytes.Buffer
	assert.NoError(t, writeStringArr(&bufWriter, testArr))

	found, err = readStringArr(bytes.NewBuffer(bufWriter.Bytes()))
	assert.NoError(t, err)
	assert.Equal(t, testArr, found)
}

func TestSerializeCommits(t *testing.T) {
	testutils.SmallTest(t)
	testCommits := []*tiling.Commit{
		&tiling.Commit{
			CommitTime: 42,
			Hash:       "ffffffffffffffffffffffffffffffffffffffff",
			Author:     "test@test.cz",
		},
		&tiling.Commit{
			CommitTime: 43,
			Hash:       "eeeeeeeeeee",
			Author:     "test@test.cz",
		},
		&tiling.Commit{
			CommitTime: 44,
			Hash:       "aaaaaaaaaaa",
			Author:     "test@test.cz",
		},
	}

	var buf bytes.Buffer
	assert.NoError(t, writeCommits(&buf, testCommits))

	found, err := readCommits(bytes.NewBuffer(buf.Bytes()))
	assert.NoError(t, err)
	assert.Equal(t, testCommits, found)
}

func TestSerializeParamSets(t *testing.T) {
	testutils.SmallTest(t)
	testParamSet := paramtools.ParamSet(map[string][]string{})
	for _, p := range testParamsList {
		testParamSet.AddParams(p)
	}

	var buf bytes.Buffer
	keyToInt, valToInt, err := writeParamSets(&buf, testParamSet)
	assert.NoError(t, err)

	intToKey, intToVal, err := readParamSets(bytes.NewBuffer(buf.Bytes()))
	assert.NoError(t, err)

	assert.Equal(t, len(testParamSet), len(keyToInt))
	assert.Equal(t, len(keyToInt), len(intToKey))
	assert.Equal(t, len(valToInt), len(intToVal))

	for key, idx := range keyToInt {
		assert.Equal(t, key, intToKey[idx])
	}

	for val, idx := range valToInt {
		assert.Equal(t, val, intToVal[idx])
	}

	for _, testParams := range testParamsList {
		byteArr := paramsToBytes(keyToInt, valToInt, testParams)
		found, err := bytesToParams(intToKey, intToVal, byteArr)
		assert.NoError(t, err)
		assert.Equal(t, testParams, found)
	}
}

func TestIntsToBytes(t *testing.T) {
	testutils.SmallTest(t)
	data := []int{20266, 20266, 20266, 20266}
	testBytes := intsToBytes(data)
	found, err := bytesToInts(testBytes)
	assert.NoError(t, err)
	assert.Equal(t, data, found)
}

func TestSerializeTile(t *testing.T) {
	testutils.LargeTest(t)
	testDataDir := TEST_DATA_DIR
	testutils.RemoveAll(t, testDataDir)
	assert.NoError(t, gcs.DownloadTestDataFile(t, TEST_DATA_STORAGE_BUCKET, TEST_DATA_STORAGE_PATH, TEST_DATA_PATH))
	defer testutils.RemoveAll(t, testDataDir)
	tile := mocks.NewMockTileBuilderFromJson(t, TEST_DATA_PATH).GetTile()

	var buf bytes.Buffer
	digestToInt, err := writeDigests(&buf, tile.Traces)
	assert.NoError(t, err)

	intToDigest, err := readDigests(bytes.NewBuffer(buf.Bytes()))
	assert.NoError(t, err)
	assert.Equal(t, len(digestToInt), len(intToDigest))

	for digest, idx := range digestToInt {
		assert.Equal(t, digest, intToDigest[idx])
	}

	buf = bytes.Buffer{}
	assert.NoError(t, SerializeTile(&buf, tile))

	foundTile, err := DeserializeTile(bytes.NewBuffer(buf.Bytes()))
	assert.NoError(t, err)
	assert.Equal(t, len(tile.Traces), len(foundTile.Traces))
	assert.Equal(t, tile.Commits, foundTile.Commits)

	// NOTE(stephana): Not comparing ParamSet because it is inconsitent with
	// the parameters of the traces.

	for id, trace := range tile.Traces {
		foundTrace, ok := foundTile.Traces[id]
		assert.True(t, ok)
		assert.Equal(t, trace, foundTrace)
	}
}

package poprepo

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"go.skia.org/infra/go/git"
	"go.skia.org/infra/go/git/testutils"
	testsize "go.skia.org/infra/go/testutils"

	"github.com/stretchr/testify/assert"
)

func TestAdd(t *testing.T) {
	testsize.MediumTest(t)

	// Create a test repo.
	gb := testutils.GitInit(t)
	defer gb.Cleanup()

	// Populate it with an initial BUILDID file.
	gb.Add(BUILDID_FILENAME, "0 0")
	gb.CommitMsg("https://android-ingest.skia.org/r/0")

	// Create a branch and check it out, otherwise we can't push
	// to 'master' on this repo.
	gb.CreateBranchTrackBranch("somebranch", "origin/master")
	gb.CheckoutBranch("somebranch")

	// Create tmp dir that gets cleaned up.
	workdir, err := ioutil.TempDir("", "poprepo")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(workdir)
	}()

	// Start testing.
	checkout, err := git.NewCheckout(gb.Dir(), workdir)
	assert.NoError(t, err)
	err = checkout.Cleanup()
	assert.NoError(t, err)

	p := NewPopRepo(checkout, true, "android-ingest")
	assert.NoError(t, err)

	_, err = p.checkout.Git("config", "user.email", "tester@example.com")
	assert.NoError(t, err)
	_, err = p.checkout.Git("config", "user.name", "tester@example.com")
	assert.NoError(t, err)

	// Confirm our inital commit is really there.
	buildid, ts, hash, err := p.GetLast()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), buildid)
	assert.Equal(t, int64(0), ts)
	assert.Len(t, hash, 40)

	// Add a couple more commits.
	err = p.Add(3516196, 1479855768)
	assert.NoError(t, err)

	err = p.Add(3516727, 1479863307)
	assert.NoError(t, err)

	// Try to add something wrong.
	err = p.Add(3516727-1, 1479863307-1)
	assert.Error(t, err)

	// Confirm we get what we added before the error.
	buildid, ts, hash, err = p.GetLast()
	assert.Equal(t, int64(3516727), buildid)
	assert.Equal(t, int64(1479863307), ts)
	assert.Len(t, hash, 40)

	// Confirm all the commits are there.
	log, err := p.checkout.Git("log", "--pretty=oneline")
	assert.NoError(t, err)

	loglines := strings.Split(log, "\n")
	// Should look something like:
	//
	//   5f28cdc83afdcc48ce45a7f2acf198542b6f4352 https://android-ingest.skia.org/r/3516727
	//   18f71105e08ff4eb7b789d9e43e08ebf14a7aef2 https://android-ingest.skia.org/r/3516196
	//   dba78253fd59d5411348f1b97068542290423391 init
	//
	// I.e. the commit subject is the redirector URL.
	//
	assert.Len(t, loglines, 4) // 3 commits with newlines gives for strings.
	assert.Contains(t, loglines[1], "https://android-ingest.skia.org/r/3516196")
}

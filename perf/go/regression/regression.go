// Package regression provides for tracking Perf regressions.
package regression

import (
	"encoding/json"
	"errors"
	"sync"

	"go.skia.org/infra/perf/go/clustering2"
	"go.skia.org/infra/perf/go/dataframe"
)

var ErrNoClusterFound = errors.New("No Cluster.")

// Status is used in TriageStatus.
type Status string

// Status constants.
const (
	NONE      Status = ""          // There is no regression.
	POSITIVE  Status = "positive"  // This change in performance is OK/expected.
	NEGATIVE  Status = "negative"  // This regression is a bug.
	UNTRIAGED Status = "untriaged" // The regression has not been triaged.
)

// Regressions is a map[query]Regression and one Regressions is stored for each
// CommitID if any regressions are found.
type Regressions struct {
	ByQuery map[string]*Regression `json:"by_query"`
	mutex   sync.Mutex
}

type TriageStatus struct {
	Status  Status `json:"status"`
	Message string `json:"message"`
}

// Regression tracks the status of the Low and High regression clusters, if they
// exist for a given CommitID and query.
//
// Note that Low and High can be nil if no regression has been found in that
// direction.
type Regression struct {
	Low        *clustering2.ClusterSummary `json:"low"`   // Can be nil.
	High       *clustering2.ClusterSummary `json:"high"`  // Can be nil.
	Frame      *dataframe.FrameResponse    `json:"frame"` // Describes the Low and High ClusterSummary's.
	LowStatus  TriageStatus                `json:"low_status"`
	HighStatus TriageStatus                `json:"high_status"`
}

func newRegression() *Regression {
	return &Regression{
		LowStatus: TriageStatus{
			Status: NONE,
		},
		HighStatus: TriageStatus{
			Status: NONE,
		},
	}
}

func New() *Regressions {
	return &Regressions{
		ByQuery: map[string]*Regression{},
	}
}

// SetLow sets the cluster for a low regression.
func (r *Regressions) SetLow(query string, df *dataframe.FrameResponse, low *clustering2.ClusterSummary) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	reg, ok := r.ByQuery[query]
	if !ok {
		reg = newRegression()
		r.ByQuery[query] = reg
	}
	if reg.Frame == nil {
		reg.Frame = df
	}
	// TODO(jcgregorio) Add checks so that we only overwrite a cluster if the new
	// cluster is 'better', for some definition of 'better'.
	reg.Low = low
	if reg.LowStatus.Status == NONE {
		reg.LowStatus.Status = UNTRIAGED
	}
}

// SetHigh sets the cluster for a high regression.
func (r *Regressions) SetHigh(query string, df *dataframe.FrameResponse, high *clustering2.ClusterSummary) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	reg, ok := r.ByQuery[query]
	if !ok {
		reg = newRegression()
		r.ByQuery[query] = reg
	}
	if reg.Frame == nil {
		reg.Frame = df
	}
	// TODO(jcgregorio) Add checks so that we only overwrite a cluster if the new
	// cluster is 'better', for some definition of 'better'.
	reg.High = high
	if reg.HighStatus.Status == NONE {
		reg.HighStatus.Status = UNTRIAGED
	}
}

// TriageLow sets the triage status for the low cluster.
func (r *Regressions) TriageLow(query string, tr TriageStatus) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	reg, ok := r.ByQuery[query]
	if !ok {
		return ErrNoClusterFound
	}
	if reg.Low == nil {
		return ErrNoClusterFound
	}
	reg.LowStatus = tr
	return nil
}

// TriageHigh sets the triage status for the high cluster.
func (r *Regressions) TriageHigh(query string, tr TriageStatus) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	reg, ok := r.ByQuery[query]
	if !ok {
		return ErrNoClusterFound
	}
	if reg.High == nil {
		return ErrNoClusterFound
	}
	reg.HighStatus = tr
	return nil
}

// Triaged returns true if all clusters are triaged.
func (r *Regressions) Triaged() bool {
	ret := true
	for _, reg := range r.ByQuery {
		ret = ret && (reg.HighStatus.Status != UNTRIAGED)
		ret = ret && (reg.LowStatus.Status != UNTRIAGED)
	}
	return ret
}

// JSON returns the Regressions serialized as JSON. Use this instead of
// serializing Regression directly as it holds the mutex while serializing.
func (r *Regressions) JSON() ([]byte, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return json.Marshal(r)
}

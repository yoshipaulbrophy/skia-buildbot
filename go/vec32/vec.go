// Some basic functions on slices of float32.
package vec32

import (
	"fmt"
	"math"
)

const (
	// MISSING_DATA_SENTINEL signifies a missing sample value.
	//
	// JSON doesn't support NaN or +/- Inf, so we need a valid float32 to signal
	// missing data that also has a compact JSON representation.
	MISSING_DATA_SENTINEL float32 = 1e32
)

func MeanAndStdDev(a []float32) (float32, float32, error) {
	count := 0
	sum := float32(0.0)
	for _, x := range a {
		if x != MISSING_DATA_SENTINEL {
			count += 1
			sum += x
		}
	}

	if count == 0 {
		return 0, 0, fmt.Errorf("Slice of length zero.")
	}
	mean := sum / float32(count)

	vr := float32(0.0)
	for _, x := range a {
		if x != MISSING_DATA_SENTINEL {
			vr += (x - mean) * (x - mean)
		}
	}
	stddev := float32(math.Sqrt(float64(vr / float32(count))))

	return mean, stddev, nil
}

// Norm normalizes the slice to a mean of 0 and a standard deviation of 1.0.
// The minStdDev is the minimum standard deviation that is normalized. Slices
// with a standard deviation less than that are not normalized for variance.
func Norm(a []float32, minStdDev float32) {
	mean, stddev, err := MeanAndStdDev(a)
	if err != nil {
		return
	}
	// Normalize the data to a mean of 0 and standard deviation of 1.0.
	for i, x := range a {
		if x != MISSING_DATA_SENTINEL {
			newX := x - mean
			if stddev > minStdDev {
				newX = newX / stddev
			}
			a[i] = newX
		}
	}
}

// Fill in non-sentinel values with nearby points.
//
// Sentinel values are filled with points later in the array, except for the
// end of the array where we can't do that, so we fill those points in
// using the first non sentinel found when searching backwards from the end.
//
// So
//    [1e32, 1e32,   2, 3, 1e32, 5]
// becomes
//    [2,    2,      2, 3, 5,    5]
//
// and
//    [3, 1e32, 5, 1e32, 1e32]
// becomes
//    [3, 5,    5, 5,    5]
//
//
// Note that a vector filled with all sentinels will be filled with 0s.
func Fill(a []float32) {
	// Find the first non-sentinel data point.
	last := float32(0.0)
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] != MISSING_DATA_SENTINEL {
			last = a[i]
			break
		}
	}
	// Now fill.
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] == MISSING_DATA_SENTINEL {
			a[i] = last
		} else {
			last = a[i]
		}
	}
}

// FillAt returns the value at the given index of a vector, using non-sentinel
// values with nearby points if the original is MISSING_DATA_SENTINEL.
//
// Note that the input vector is unchanged.
//
// Returns non-nil error if the given index is out of bounds.
func FillAt(a []float32, i int) (float32, error) {
	l := len(a)
	if i < 0 || i >= l {
		return 0, fmt.Errorf("FillAt index %d out of bound %d.\n", i, l)
	}
	b := make([]float32, l, l)
	copy(b, a)
	Fill(b)
	return b[i], nil
}

func Dup(a []float32) []float32 {
	ret := make([]float32, len(a), len(a))
	copy(ret, a)
	return ret
}

// Mean calculates and returns the Mean value of the given []float32.
//
// Returns 0 for an array with no non-MISSING_DATA_SENTINEL values.
func Mean(xs []float32) float32 {
	total := float32(0.0)
	n := 0
	for _, v := range xs {
		if v != MISSING_DATA_SENTINEL {
			total += v
			n++
		}
	}
	if n == 0 {
		return total
	} else {
		return total / float32(n)
	}
}

// MeanMissing calculates and returns the Mean value of the given []float32.
//
// Returns MISSING_DATA_SENTINEL for an array with all MISSING_DATA_SENTINEL values.
func MeanMissing(xs []float32) float32 {
	total := float32(0.0)
	n := 0
	for _, v := range xs {
		if v != MISSING_DATA_SENTINEL {
			total += v
			n++
		}
	}
	if n == 0 {
		return MISSING_DATA_SENTINEL
	} else {
		return total / float32(n)
	}
}

// FillMeanMissing fills the slice with the mean of all the values in the slice
// using MeanMissing.
func FillMeanMissing(a []float32) {
	value := MeanMissing(a)
	// Now fill.
	for i, _ := range a {
		a[i] = value
	}
}

// FillStdDev fills the slice with the Standard Deviation of the values in the slice.
//
// If slice is filled with only MISSING_DATA_SENTINEL then the slice will be
// filled with MISSING_DATA_SENTINEL.
func FillStdDev(a []float32) {
	_, stddev, err := MeanAndStdDev(a)
	if err != nil {
		stddev = MISSING_DATA_SENTINEL
	}
	// Now fill.
	for i, _ := range a {
		a[i] = stddev
	}
}

// FillCov fills the slice with the Coefficient of Variation of the values in the slice.
//
// If the mean is 0 or the slice is filled with only MISSING_DATA_SENTINEL then
// the slice will be filled with MISSING_DATA_SENTINEL.
func FillCov(a []float32) {
	mean, stddev, err := MeanAndStdDev(a)
	cov := MISSING_DATA_SENTINEL
	if err == nil {
		cov = stddev / mean
	}
	if math.IsNaN(float64(cov)) {
		cov = MISSING_DATA_SENTINEL
	}
	// Now fill.
	for i, _ := range a {
		a[i] = cov
	}
}

// SSE calculates and returns the sum squared error from the given base of []float32.
//
// Returns 0 for an array with no non-MISSING_DATA_SENTINEL values.
func SSE(xs []float32, base float32) float32 {
	total := float32(0.0)
	for _, v := range xs {
		if v != MISSING_DATA_SENTINEL {
			total += (v - base) * (v - base)
		}
	}
	return total
}

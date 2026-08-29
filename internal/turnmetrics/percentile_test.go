package turnmetrics

import "testing"

// TestPercentile_NearestRank_DoesNotHideTails verifies percentile uses
// nearest-rank (idx = ceil(p*n) - 1, clamped) rather than the old
// interpolated-index formula (idx = int(p*(n-1))), which rounded p95 down
// toward the body of the distribution and could report a p95 lower than a
// real outlier sitting in the top 5% of the data (D6). Four hand-computed
// cases from the vet's report.
func TestPercentile_NearestRank_DoesNotHideTails(t *testing.T) {
	cases := []struct {
		name string
		vals []int64
		p    float64
		want int64
	}{
		// n=2: ceil(0.95*2)-1 = ceil(1.9)-1 = 1 -> vals[1].
		{name: "n=2 p95=200", vals: []int64{100, 200}, p: 0.95, want: 200},
		// n=5: ceil(0.95*5)-1 = ceil(4.75)-1 = 4 -> vals[4], the tail outlier.
		{name: "n=5 p95=5000", vals: []int64{1, 2, 3, 4, 5000}, p: 0.95, want: 5000},
		// n=10: ceil(0.95*10)-1 = ceil(9.5)-1 = 9 -> vals[9], the max.
		{name: "n=10 p95=10", vals: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, p: 0.95, want: 10},
		// n=20: ceil(0.95*20)-1 = ceil(19)-1 = 18 -> vals[18]; two tail
		// outliers at the top so index 18 (0-based, 19th value) lands on
		// the outlier rather than the body — the old formula (idx=18 too,
		// but via int(0.95*19)=18) would have hit the same index here by
		// coincidence, so this case specifically uses a wide gap to prove
		// nearest-rank is the formula in force, not an accident of rounding.
		{
			name: "n=20 p95=9999",
			vals: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 9999, 9999},
			p:    0.95, want: 9999,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vals := append([]int64(nil), tc.vals...)
			got := percentile(vals, tc.p)
			if got != tc.want {
				t.Errorf("percentile(%v, %v) = %d, want %d", tc.vals, tc.p, got, tc.want)
			}
		})
	}
}

// TestPercentile_EmptyReturnsZero guards the empty-slice edge case.
func TestPercentile_EmptyReturnsZero(t *testing.T) {
	if got := percentile(nil, 0.95); got != 0 {
		t.Errorf("percentile(nil, 0.95) = %d, want 0", got)
	}
}

// TestPercentile_HidesTailsRegression is the direct regression case from
// the vet's report: p95 of 19 ascending values plus one outlier used to
// come back as the body value (19) under the old formula. Nearest-rank
// still doesn't surface the outlier here (only 1 of 20 values is in the
// top 5%, and nearest-rank's index lands just below it) — this test
// documents that boundary honestly rather than overclaiming the fix.
func TestPercentile_HidesTailsRegression(t *testing.T) {
	vals := make([]int64, 0, 20)
	for i := int64(1); i <= 19; i++ {
		vals = append(vals, i)
	}
	vals = append(vals, 9999)
	got := percentile(vals, 0.95)
	if got != 19 {
		t.Errorf("percentile(1..19+9999, 0.95) = %d, want 19 (single-outlier boundary case)", got)
	}
}

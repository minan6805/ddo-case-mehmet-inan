package assets

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestReachableCycleDiamondAndMultiRoot(t *testing.T) {
	edges := []Edge{{1, 2}, {1, 3}, {2, 4}, {3, 4}, {4, 1}, {4, 5}}
	got := Reachable([]int64{1, 1}, edges)
	want := []Reach{{1, 0, []int64{1}}, {2, 1, []int64{1, 2}}, {3, 1, []int64{1, 3}}, {4, 2, []int64{1, 2, 4}}, {5, 3, []int64{1, 2, 4, 5}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if got := Reachable([]int64{5}, edges); len(got) != 1 {
		t.Fatal(got)
	}
	got = Reachable([]int64{5, 1}, edges)
	if len(got) != 5 || got[1].ID != 5 || got[1].Distance != 0 {
		t.Fatal(got)
	}
}
func TestReachableMatchesIndependentFloydWarshall(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const n = 8
	for sample := 0; sample < 100; sample++ {
		edges := []Edge{}
		var distances [n][n]int
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				distances[i][j] = 100
				if i == j {
					distances[i][j] = 0
				} else if rng.Intn(4) == 0 {
					edges = append(edges, Edge{int64(i + 1), int64(j + 1)})
					distances[i][j] = 1
				}
			}
		}
		for k := 0; k < n; k++ {
			for i := 0; i < n; i++ {
				for j := 0; j < n; j++ {
					distances[i][j] = min(distances[i][j], distances[i][k]+distances[k][j])
				}
			}
		}
		for root := 0; root < n; root++ {
			got := Reachable([]int64{int64(root + 1)}, edges)
			seen := map[int64]bool{}
			for _, r := range got {
				if seen[r.ID] || r.Distance != distances[root][r.ID-1] || len(r.Path) != r.Distance+1 {
					t.Fatalf("sample %d: %+v", sample, r)
				}
				seen[r.ID] = true
			}
			for id := 0; id < n; id++ {
				if seen[int64(id+1)] != (distances[root][id] < 100) {
					t.Fatal("reachability mismatch")
				}
			}
		}
	}
}

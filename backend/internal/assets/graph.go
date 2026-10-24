package assets

import "sort"

type Edge struct {
	ProviderID  int64 `json:"provider_id"`
	DependentID int64 `json:"dependent_id"`
}
type Reach struct {
	ID       int64   `json:"id"`
	Distance int     `json:"distance"`
	Path     []int64 `json:"path"`
}

// Reachable follows provider -> dependent, including roots. A visited set makes
// cycles finite and diamonds unique. Sorted roots/neighbours select stable shortest paths.
func Reachable(roots []int64, edges []Edge) []Reach {
	roots = append([]int64{}, roots...)
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	next := map[int64][]int64{}
	for _, e := range edges {
		next[e.ProviderID] = append(next[e.ProviderID], e.DependentID)
	}
	for id := range next {
		sort.Slice(next[id], func(i, j int) bool { return next[id][i] < next[id][j] })
	}
	seen := map[int64]bool{}
	queue := []Reach{}
	for _, id := range roots {
		if !seen[id] {
			seen[id] = true
			queue = append(queue, Reach{ID: id, Path: []int64{id}})
		}
	}
	for i := 0; i < len(queue); i++ {
		current := queue[i]
		for _, id := range next[current.ID] {
			if seen[id] {
				continue
			}
			seen[id] = true
			path := append(append([]int64{}, current.Path...), id)
			queue = append(queue, Reach{ID: id, Distance: current.Distance + 1, Path: path})
		}
	}
	return queue
}

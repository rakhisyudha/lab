package content

import "github.com/google/uuid"

// HasCycle melaporkan apakah graf prasyarat Topic mengandung siklus
// (Requirement 22.6). Graf direpresentasikan sebagai topic -> prasyarat.
func HasCycle(graph map[uuid.UUID][]uuid.UUID) bool {
	const (
		visiting = 1
		visited  = 2
	)
	state := map[uuid.UUID]int{}
	var visit func(uuid.UUID) bool
	visit = func(n uuid.UUID) bool {
		if state[n] == visiting {
			return true
		}
		if state[n] == visited {
			return false
		}
		state[n] = visiting
		for _, next := range graph[n] {
			if visit(next) {
				return true
			}
		}
		state[n] = visited
		return false
	}
	for n := range graph {
		if visit(n) {
			return true
		}
	}
	return false
}

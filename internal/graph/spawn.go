package graph

import (
	"fmt"
	"strings"

	"github.com/mawkeye/flowstep/types"
)

// DetectSpawnCycles checks whether the set of registered definitions forms a cycle
// in the cross-workflow spawn graph. It returns ErrSpawnCycle (from sentinels) if
// a cycle is found, nil otherwise.
//
// The spawn graph is keyed on Definition.WorkflowType (matching ChildDef.WorkflowType /
// ChildrenDef.WorkflowType). AggregateType is the registry key but is NOT used here.
func DetectSpawnCycles(definitions []*types.Definition, sentinels Sentinels) error {
	// Build adjacency: workflowType → set of spawned workflowTypes
	adj := make(map[string][]string, len(definitions))
	for _, def := range definitions {
		if _, ok := adj[def.WorkflowType]; !ok {
			adj[def.WorkflowType] = nil
		}
		for _, tr := range def.Transitions {
			if tr.ChildDef != nil && tr.ChildDef.WorkflowType != "" {
				adj[def.WorkflowType] = append(adj[def.WorkflowType], tr.ChildDef.WorkflowType)
			}
			if tr.ChildrenDef != nil && tr.ChildrenDef.WorkflowType != "" {
				adj[def.WorkflowType] = append(adj[def.WorkflowType], tr.ChildrenDef.WorkflowType)
			}
		}
	}

	// DFS cycle detection using three-colour marking:
	//   0 = white (unvisited), 1 = grey (in current path), 2 = black (done)
	colour := make(map[string]int, len(adj))
	var path []string // current DFS stack for error reporting

	var dfs func(node string) error
	dfs = func(node string) error {
		colour[node] = 1 // grey — in current path
		path = append(path, node)

		for _, neighbour := range adj[node] {
			switch colour[neighbour] {
			case 1: // back edge → cycle
				// Find where the cycle starts in path
				cycleStart := -1
				for i, n := range path {
					if n == neighbour {
						cycleStart = i
						break
					}
				}
				cyclePath := path[cycleStart:]
				cycleStr := strings.Join(cyclePath, " → ") + " → " + neighbour
				return &ValidationError{
					Sentinel: sentinels.ErrSpawnCycle,
					Detail:   fmt.Sprintf("flowstep: spawn cycle detected: %s", cycleStr),
				}
			case 0: // unvisited — recurse
				if err := dfs(neighbour); err != nil {
					return err
				}
			// case 2: already fully explored — no cycle through this node
			}
		}

		path = path[:len(path)-1]
		colour[node] = 2 // black — done
		return nil
	}

	for node := range adj {
		if colour[node] == 0 {
			if err := dfs(node); err != nil {
				return err
			}
		}
	}
	return nil
}

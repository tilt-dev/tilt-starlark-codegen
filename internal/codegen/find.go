package codegen

import (
	"sort"

	"k8s.io/gengo/types"
)

// Find all the member types that need custom unpackers.
func FindStructMembers(topLevelTypes []*types.Type) ([]*types.Type, error) {
	resultMap := map[string]*types.Type{}
	for _, t := range topLevelTypes {
		spec := getSpecMemberType(t)
		if spec == nil {
			continue
		}
		findStructMembersHelper(spec, resultMap)
	}

	result := []*types.Type{}
	for _, t := range resultMap {
		result = append(result, t)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name.Name < result[j].Name.Name
	})
	return result, nil
}

// A recursive helper that populates the map with the results if its search.
func findStructMembersHelper(t *types.Type, result map[string]*types.Type) {
	recurse := func(candidate *types.Type) {
		_, exists := result[candidate.Name.Name]
		if exists {
			return
		}
		result[candidate.Name.Name] = candidate
		findStructMembersHelper(candidate, result)
	}

	for _, m := range t.Members {
		if isTimeMember(m) {
			continue
		}

		if m.Type.Kind == types.Struct {
			recurse(m.Type)
		}

		if (m.Type.Kind == types.Slice || m.Type.Kind == types.Pointer) && m.Type.Elem.Kind == types.Struct {
			recurse(m.Type.Elem)
		}
	}
}

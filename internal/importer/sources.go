package importer

import "strings"

func sourceRefFromNode(node ManagedNode) NodeSourceRef {
	return NodeSourceRef{
		TagPrefix:      strings.TrimSpace(node.TagPrefix),
		ImportID:       strings.TrimSpace(node.ImportID),
		Mode:           strings.TrimSpace(node.ImportMode),
		Source:         strings.TrimSpace(node.ImportSource),
		Format:         strings.TrimSpace(node.ImportFormat),
		ChainProfileID: strings.TrimSpace(node.ChainProfileID),
	}
}

func nodeSourceRefs(node ManagedNode) []NodeSourceRef {
	refs := make([]NodeSourceRef, 0, len(node.SourceRefs)+1)
	seen := make(map[string]struct{}, len(node.SourceRefs)+1)
	appendRef := func(ref NodeSourceRef) {
		ref = normalizeSourceRef(ref)
		key := sourceRefIdentity(ref)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	for _, ref := range node.SourceRefs {
		appendRef(ref)
	}
	appendRef(sourceRefFromNode(node))
	return refs
}

func mergeNodeSourceRefs(existing, incoming ManagedNode) ManagedNode {
	refs := append(nodeSourceRefs(existing), nodeSourceRefs(incoming)...)
	incoming.SourceRefs = deduplicateSourceRefs(refs)
	return incoming
}

func deduplicateSourceRefs(refs []NodeSourceRef) []NodeSourceRef {
	result := make([]NodeSourceRef, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref = normalizeSourceRef(ref)
		key := sourceRefIdentity(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func normalizeSourceRef(ref NodeSourceRef) NodeSourceRef {
	ref.TagPrefix = strings.TrimSpace(ref.TagPrefix)
	ref.ImportID = strings.TrimSpace(ref.ImportID)
	ref.Mode = strings.TrimSpace(ref.Mode)
	ref.Source = strings.TrimSpace(ref.Source)
	ref.Format = strings.TrimSpace(ref.Format)
	ref.ChainProfileID = strings.TrimSpace(ref.ChainProfileID)
	ref.FetchPolicy = strings.TrimSpace(ref.FetchPolicy)
	if ref.Mode == "" && isURLSourceRef(ref) {
		ref.Mode = "url"
	}
	if isURLSourceRef(ref) {
		ref.FetchPolicy = FetchAuto
	}
	return ref
}

func sourceRefIdentity(ref NodeSourceRef) string {
	tag := strings.TrimSpace(ref.TagPrefix)
	mode := strings.TrimSpace(ref.Mode)
	source := strings.TrimSpace(ref.Source)
	if tag == "" && mode == "" && source == "" && strings.TrimSpace(ref.ImportID) == "" {
		return ""
	}
	if isURLSourceRef(ref) {
		return tag + "\x00url\x00" + source + "\x00" + ref.ChainProfileID
	}
	return tag + "\x00" + mode + "\x00" + source + "\x00" + strings.TrimSpace(ref.ImportID) + "\x00" + ref.ChainProfileID
}

func isURLSourceRef(ref NodeSourceRef) bool {
	mode := strings.TrimSpace(ref.Mode)
	if mode == "url" {
		return true
	}
	if mode != "" {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(ref.Source))
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func applyPrimarySource(node *ManagedNode, ref NodeSourceRef) {
	node.TagPrefix = ref.TagPrefix
	node.ImportID = ref.ImportID
	node.ImportMode = ref.Mode
	node.ImportSource = ref.Source
	node.ImportFormat = ref.Format
	node.ChainProfileID = ref.ChainProfileID
}

func nodeHasSource(node ManagedNode, match func(NodeSourceRef) bool) bool {
	for _, ref := range nodeSourceRefs(node) {
		if match(ref) {
			return true
		}
	}
	return false
}

func nodeWithSource(node ManagedNode, ref NodeSourceRef) ManagedNode {
	applyPrimarySource(&node, ref)
	return node
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

package main

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
)

type TypeScriptAnalyzer struct {
	moduleByPath map[string]NodeID
	namedExports map[NodeID]map[string][]NodeID
}

type typeScriptImport struct {
	Names []string
	Spec  string
}

type typeScriptReExport struct {
	Names     []string
	Spec      string
	ExportAll bool
}

func NewTypeScriptAnalyzer(graph *Graph) (*TypeScriptAnalyzer, error) {
	analyzer := &TypeScriptAnalyzer{
		moduleByPath: make(map[string]NodeID),
		namedExports: make(map[NodeID]map[string][]NodeID),
	}
	contents := make(map[NodeID]string)
	directExports := make(map[NodeID][]string)
	reExportsByFile := make(map[NodeID][]typeScriptReExport)

	for nodeID, node := range graph.Nodes {
		if !isTypeScriptLikeLanguage(node.Language) {
			continue
		}
		registerTypeScriptModulePath(analyzer.moduleByPath, nodeID)
		content, err := os.ReadFile(node.Path)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", node.Path, err)
		}
		contents[nodeID] = string(content)
		directExports[nodeID] = parseTypeScriptDirectExports(string(content))
		reExportsByFile[nodeID] = parseTypeScriptReExports(string(content))
		for _, name := range directExports[nodeID] {
			analyzer.addNamedExport(nodeID, name, nodeID)
		}
	}

	for nodeID, reExports := range reExportsByFile {
		for _, reExport := range reExports {
			target, ok := analyzer.resolveImport(nodeID, reExport.Spec)
			if !ok {
				continue
			}
			if reExport.ExportAll {
				for _, name := range directExports[target] {
					analyzer.addNamedExport(nodeID, name, target)
				}
				continue
			}
			for _, name := range reExport.Names {
				analyzer.addNamedExport(nodeID, name, target)
			}
		}
	}

	_ = contents
	return analyzer, nil
}

func (analyzer *TypeScriptAnalyzer) CanAnalyze(language string) bool {
	return isTypeScriptLikeLanguage(language)
}

func (analyzer *TypeScriptAnalyzer) FindConnections(
	root string,
	file SourceFile,
	graph *Graph,
) ([]Edge, error) {
	from, err := nodeIDForSourceFile(root, file)
	if err != nil {
		return nil, err
	}
	if _, exists := graph.Nodes[from]; !exists {
		return nil, fmt.Errorf("source node %q does not exist", from)
	}
	content, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, fmt.Errorf("read TypeScript source: %w", err)
	}

	seenTargets := make(map[NodeID]struct{})
	var edges []Edge
	addEdge := func(to NodeID) {
		if to == "" || to == from {
			return
		}
		if _, seen := seenTargets[to]; seen {
			return
		}
		seenTargets[to] = struct{}{}
		edges = append(edges, Edge{From: from, To: to, Kind: EdgeKindImports})
	}

	for _, importDeclaration := range parseTypeScriptImports(string(content)) {
		target, ok := analyzer.resolveImport(from, importDeclaration.Spec)
		if !ok {
			continue
		}
		targets := analyzer.resolveImportedNames(target, importDeclaration.Names)
		if len(targets) == 0 {
			addEdge(target)
			continue
		}
		for _, namedTarget := range targets {
			addEdge(namedTarget)
		}
	}
	for _, reExport := range parseTypeScriptReExports(string(content)) {
		target, ok := analyzer.resolveImport(from, reExport.Spec)
		if ok {
			addEdge(target)
		}
	}

	return edges, nil
}

func (analyzer *TypeScriptAnalyzer) resolveImportedNames(module NodeID, names []string) []NodeID {
	if len(names) == 0 {
		return nil
	}
	exports := analyzer.namedExports[module]
	if len(exports) == 0 {
		return nil
	}
	var targets []NodeID
	for _, name := range names {
		targets = append(targets, exports[name]...)
	}
	return uniqueNodeIDs(targets)
}

func (analyzer *TypeScriptAnalyzer) resolveImport(from NodeID, spec string) (NodeID, bool) {
	if !strings.HasPrefix(spec, ".") {
		return "", false
	}
	base := pathpkg.Dir(string(from))
	candidate := pathpkg.Clean(pathpkg.Join(base, spec))
	if target, exists := analyzer.moduleByPath[candidate]; exists {
		return target, true
	}
	return "", false
}

func (analyzer *TypeScriptAnalyzer) addNamedExport(module NodeID, name string, target NodeID) {
	name = strings.TrimSpace(name)
	if name == "" || target == "" {
		return
	}
	exports := analyzer.namedExports[module]
	if exports == nil {
		exports = make(map[string][]NodeID)
		analyzer.namedExports[module] = exports
	}
	exports[name] = uniqueNodeIDs(append(exports[name], target))
}

func isTypeScriptLikeLanguage(language string) bool {
	return language == "TypeScript" || language == "JavaScript"
}

func registerTypeScriptModulePath(modules map[string]NodeID, nodeID NodeID) {
	id := string(nodeID)
	withoutExtension := strings.TrimSuffix(id, pathpkg.Ext(id))
	modules[id] = nodeID
	modules[withoutExtension] = nodeID
	if pathpkg.Base(withoutExtension) == "index" {
		modules[pathpkg.Dir(withoutExtension)] = nodeID
	}
}

func nodeIDForSourceFile(root string, file SourceFile) (NodeID, error) {
	relativePath, err := filepath.Rel(root, file.Path)
	if err != nil {
		return "", fmt.Errorf("make source path relative: %w", err)
	}
	return NodeID(filepath.ToSlash(relativePath)), nil
}

func parseTypeScriptImports(content string) []typeScriptImport {
	content = stripTypeScriptComments(content)
	var imports []typeScriptImport

	fromPattern := regexp.MustCompile(`(?s)\bimport\s+(?:type\s+)?(.+?)\s+from\s+['"]([^'"]+)['"]`)
	for _, match := range fromPattern.FindAllStringSubmatch(content, -1) {
		imports = append(imports, typeScriptImport{
			Names: parseTypeScriptNamedList(match[1], false),
			Spec:  strings.TrimSpace(match[2]),
		})
	}

	sideEffectPattern := regexp.MustCompile(`(?m)\bimport\s+['"]([^'"]+)['"]`)
	for _, match := range sideEffectPattern.FindAllStringSubmatch(content, -1) {
		imports = append(imports, typeScriptImport{Spec: strings.TrimSpace(match[1])})
	}
	return imports
}

func parseTypeScriptDirectExports(content string) []string {
	content = stripTypeScriptComments(content)
	pattern := regexp.MustCompile(`(?m)\bexport\s+(?:default\s+)?(?:async\s+)?(?:function|class|interface|type|const|let|var|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	var names []string
	for _, match := range pattern.FindAllStringSubmatch(content, -1) {
		names = append(names, match[1])
	}
	return uniqueSortedStrings(names)
}

func parseTypeScriptReExports(content string) []typeScriptReExport {
	content = stripTypeScriptComments(content)
	var exports []typeScriptReExport

	namedPattern := regexp.MustCompile(`(?s)\bexport\s+(?:type\s+)?\{([^}]*)\}\s+from\s+['"]([^'"]+)['"]`)
	for _, match := range namedPattern.FindAllStringSubmatch(content, -1) {
		exports = append(exports, typeScriptReExport{
			Names: parseTypeScriptNamedList(match[1], true),
			Spec:  strings.TrimSpace(match[2]),
		})
	}

	allPattern := regexp.MustCompile(`(?m)\bexport\s+\*\s+from\s+['"]([^'"]+)['"]`)
	for _, match := range allPattern.FindAllStringSubmatch(content, -1) {
		exports = append(exports, typeScriptReExport{
			Spec:      strings.TrimSpace(match[1]),
			ExportAll: true,
		})
	}
	return exports
}

func parseTypeScriptNamedList(content string, exportedNames bool) []string {
	if open := strings.Index(content, "{"); open >= 0 {
		if close := strings.LastIndex(content, "}"); close > open {
			content = content[open+1 : close]
		}
	}
	var names []string
	for _, part := range strings.Split(content, ",") {
		name := strings.TrimSpace(part)
		name = strings.TrimPrefix(name, "type ")
		fields := strings.Fields(name)
		if len(fields) == 0 {
			continue
		}
		if len(fields) >= 3 && fields[1] == "as" {
			if exportedNames {
				names = append(names, fields[2])
			} else {
				names = append(names, fields[0])
			}
			continue
		}
		names = append(names, fields[0])
	}
	return uniqueSortedStrings(names)
}

func stripTypeScriptComments(content string) string {
	blockPattern := regexp.MustCompile(`(?s)/\*.*?\*/`)
	linePattern := regexp.MustCompile(`(?m)//.*$`)
	return linePattern.ReplaceAllString(blockPattern.ReplaceAllString(content, ""), "")
}

func uniqueNodeIDs(values []NodeID) []NodeID {
	seen := make(map[NodeID]bool, len(values))
	result := make([]NodeID, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

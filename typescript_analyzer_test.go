package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTypeScriptAnalyzerConnectsNamedImportsThroughBarrelExports(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/components/SearchField.tsx", "export function SearchField() { return null; }\n")
	writeTestFile(t, root, "src/components/index.ts", "export { SearchField } from './SearchField';\n")
	writeTestFile(t, root, "src/screens/DiscoverScreen.tsx", "import { SearchField } from '../components';\nexport function DiscoverScreen() { return <SearchField />; }\n")
	writeTestFile(t, root, "src/screens/SearchResultsScreen.tsx", "import { SearchField } from '../components';\nexport function SearchResultsScreen() { return <SearchField />; }\n")

	graph, err := analyzeRepositoryGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	assertGraphHasEdge(t, graph, "src/screens/DiscoverScreen.tsx", "src/components/SearchField.tsx", EdgeKindImports)
	assertGraphHasEdge(t, graph, "src/screens/SearchResultsScreen.tsx", "src/components/SearchField.tsx", EdgeKindImports)
	assertGraphHasEdge(t, graph, "src/components/index.ts", "src/components/SearchField.tsx", EdgeKindImports)
}

func TestTypeScriptAnalyzerConnectsDirectRelativeImports(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/components/SearchField.tsx", "export function SearchField() { return null; }\n")
	writeTestFile(t, root, "src/screens/DiscoverScreen.tsx", "import { SearchField } from '../components/SearchField';\nexport function DiscoverScreen() { return <SearchField />; }\n")

	graph, err := analyzeRepositoryGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	assertGraphHasEdge(t, graph, "src/screens/DiscoverScreen.tsx", "src/components/SearchField.tsx", EdgeKindImports)
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertGraphHasEdge(t *testing.T, graph *Graph, from, to string, kind EdgeKind) {
	t.Helper()
	for _, edge := range graph.Edges {
		if edge.From == NodeID(from) && edge.To == NodeID(to) && edge.Kind == kind {
			return
		}
	}
	t.Fatalf("missing %s edge from %q to %q; edges = %#v", kind, from, to, graph.Edges)
}

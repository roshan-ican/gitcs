package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildSelectionContextIncludesSourceAndConnections(t *testing.T) {
	root := t.TempDir()
	componentPath := filepath.Join(root, "src", "components", "SearchField.tsx")
	screenPath := filepath.Join(root, "src", "screens", "DiscoverScreen.tsx")
	if err := os.MkdirAll(filepath.Dir(componentPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(screenPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(componentPath, []byte("export function SearchField() { return null; }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(screenPath, []byte("import { SearchField } from '../components';\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := mapSnapshot{
		Response: mapResponse{
			Repository: "binder",
			Branch:     "main",
			Nodes: []mapNodeResponse{
				{
					ID:          "src/components/SearchField.tsx",
					Label:       "SearchField.tsx",
					Language:    "TypeScript",
					Description: "TypeScript source file.",
					Openable:    true,
					Activity: mapFileActivity{
						CommitsAll: 2,
						RecentCommits: []mapCommitResponse{{
							Hash:    "abc1234",
							Message: "Add search",
							Author:  "Roshan",
							When:    time.Now(),
						}},
					},
				},
				{ID: "src/screens/DiscoverScreen.tsx", Label: "DiscoverScreen.tsx", Language: "TypeScript", Description: "TypeScript source file.", Openable: true},
			},
			Edges: []mapEdgeResponse{{
				From: "src/screens/DiscoverScreen.tsx",
				To:   "src/components/SearchField.tsx",
				Kind: EdgeKindImports,
			}},
		},
		OpenTargets: map[NodeID]openTarget{
			"src/components/SearchField.tsx": {Path: componentPath, Openable: true},
			"src/screens/DiscoverScreen.tsx": {Path: screenPath, Openable: true},
		},
	}

	context, err := buildSelectionContext(root, snapshot, []NodeID{"src/components/SearchField.tsx"})
	if err != nil {
		t.Fatal(err)
	}

	assertContainsAll(t, context.Prompt, []string{
		"# Understand 1 selected file",
		"src/components/SearchField.tsx",
		"Used by: DiscoverScreen.tsx",
		"```tsx",
		"export function SearchField",
	})
	if context.FileCount != 1 || context.Truncated {
		t.Fatalf("context metadata = %#v, want one untruncated file", context)
	}
}

func TestBuildSelectionContextRejectsUnknownSelection(t *testing.T) {
	_, err := buildSelectionContext(t.TempDir(), mapSnapshot{}, []NodeID{"missing.tsx", "__frontend__"})
	if err == nil || !strings.Contains(err.Error(), "select at least one source file") {
		t.Fatalf("error = %v, want select-source-file error", err)
	}
}

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// readWorkingTreeChanges keeps go-git's two-column status representation out
// of the pure review-map transformation. A worktree change takes precedence
// over a staged change because it describes the file currently on disk.
func readWorkingTreeChanges(worktree *git.Worktree) ([]reviewChange, error) {
	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("read Git working-tree status: %w", err)
	}

	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	changes := make([]reviewChange, 0, len(paths))
	for _, path := range paths {
		fileStatus := status[path]
		code := fileStatus.Worktree
		if code == git.Unmodified {
			code = fileStatus.Staging
		}

		mapped, changed := mapGitStatus(code)
		if !changed {
			continue
		}

		changes = append(changes, reviewChange{
			Path:   filepath.ToSlash(filepath.Clean(path)),
			Status: mapped,
		})
	}

	return changes, nil
}

func readCommittedChanges(baseTree, headTree *object.Tree) ([]reviewChange, error) {
	if baseTree == nil || headTree == nil {
		return nil, nil
	}
	treeChanges, err := object.DiffTreeWithOptions(context.Background(), baseTree, headTree, object.DefaultDiffTreeOptions)
	if err != nil {
		return nil, fmt.Errorf("read committed changes: %w", err)
	}

	changes := make([]reviewChange, 0, len(treeChanges))
	for _, treeChange := range treeChanges {
		status, changed := mapTreeChangeStatus(treeChange)
		if !changed {
			continue
		}
		path := treeChange.To.Name
		if path == "" {
			path = treeChange.From.Name
		}
		changes = append(changes, reviewChange{
			Path:    cleanChangePath(path),
			OldPath: cleanChangePath(treeChange.From.Name),
			Status:  status,
		})
	}
	sortReviewChanges(changes)
	return changes, nil
}

func mergeReviewChanges(committedChanges, workingTreeChanges []reviewChange) []reviewChange {
	byPath := make(map[string]reviewChange, len(committedChanges)+len(workingTreeChanges))
	for _, change := range committedChanges {
		byPath[change.Path] = change
	}
	for _, change := range workingTreeChanges {
		existing := byPath[change.Path]
		if existing.OldPath != "" {
			change.OldPath = existing.OldPath
		}
		byPath[change.Path] = change
	}

	changes := make([]reviewChange, 0, len(byPath))
	for _, change := range byPath {
		changes = append(changes, change)
	}
	sortReviewChanges(changes)
	return changes
}

func mapGitStatus(code git.StatusCode) (changeStatus, bool) {
	switch code {
	case git.Untracked, git.Added:
		return changeAdded, true
	case git.Modified, git.Copied, git.UpdatedButUnmerged:
		return changeModified, true
	case git.Deleted:
		return changeDeleted, true
	case git.Renamed:
		return changeRenamed, true
	default:
		return "", false
	}
}

func mapTreeChangeStatus(change *object.Change) (changeStatus, bool) {
	action, err := change.Action()
	if err != nil {
		return "", false
	}
	if change.From.Name != "" && change.To.Name != "" && change.From.Name != change.To.Name {
		return changeRenamed, true
	}
	switch action {
	case merkletrie.Insert:
		return changeAdded, true
	case merkletrie.Delete:
		return changeDeleted, true
	case merkletrie.Modify:
		return changeModified, true
	default:
		return "", false
	}
}

func sortReviewChanges(changes []reviewChange) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].Status < changes[j].Status
	})
}

func cleanChangePath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func resolveRevisionTree(repo *git.Repository, revision string) (*object.Tree, error) {
	hash, err := repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return nil, fmt.Errorf("resolve revision %q: %w", revision, err)
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("read commit %q: %w", revision, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("read tree for %q: %w", revision, err)
	}
	return tree, nil
}

func analyzeRepositoryGraph(root string) (*Graph, error) {
	files, err := findSourceFiles(root)
	if err != nil {
		return nil, fmt.Errorf("could not scan the repository: %w", err)
	}
	graph, err := buildFileGraph(root, files)
	if err != nil {
		return nil, fmt.Errorf("could not build the project graph: %w", err)
	}
	goAnalyzer, err := NewGoAnalyzer(graph)
	if err != nil {
		return nil, fmt.Errorf("could not prepare the Go analyzer: %w", err)
	}
	typeScriptAnalyzer, err := NewTypeScriptAnalyzer(graph)
	if err != nil {
		return nil, fmt.Errorf("could not prepare the TypeScript analyzer: %w", err)
	}
	if err := applyAnalyzers(root, files, graph, []Analyzer{goAnalyzer, typeScriptAnalyzer}); err != nil {
		return nil, fmt.Errorf("could not analyze project connections: %w", err)
	}
	return graph, nil
}

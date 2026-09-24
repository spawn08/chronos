package builtins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type workspaceRootKey struct{}

// WithWorkspaceRoot returns a child context whose built-in filesystem and host
// shell tools resolve paths against root. The override is request-scoped and
// never mutates process state or a shared tool definition.
func WithWorkspaceRoot(ctx context.Context, root string) context.Context {
	if root == "" {
		return ctx
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return context.WithValue(ctx, workspaceRootKey{}, filepath.Clean(root))
}

// WorkspaceRootFromContext returns the request-scoped workspace root, if set.
func WorkspaceRootFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	root, ok := ctx.Value(workspaceRootKey{}).(string)
	return root, ok && root != ""
}

// WorkspaceRoot returns the request root when present, otherwise configured.
func WorkspaceRoot(ctx context.Context, configured string) string {
	if root, ok := WorkspaceRootFromContext(ctx); ok {
		return root
	}
	return configured
}

func resolveWorkspacePath(ctx context.Context, configured, path string) (string, error) {
	root, overridden := WorkspaceRootFromContext(ctx)
	if !overridden {
		root = configured
	}
	if root == "" {
		return resolvePath(configured, path), nil
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	var resolved string
	if filepath.IsAbs(path) {
		if pathWithin(root, path) {
			resolved = filepath.Clean(path)
		} else if configured != "" {
			base, baseErr := filepath.Abs(configured)
			if baseErr == nil && pathWithin(base, path) {
				relative, relErr := filepath.Rel(base, path)
				if relErr == nil {
					resolved = filepath.Join(root, relative)
				}
			}
		}
		if resolved == "" {
			return "", fmt.Errorf("path %q is outside request workspace", path)
		}
	} else {
		resolved = filepath.Join(root, path)
		if !pathWithin(root, resolved) {
			return "", fmt.Errorf("path %q is outside request workspace", path)
		}
	}

	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace symlinks: %w", err)
	}
	canonicalPath, err := evalSymlinksAllowMissing(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}
	if !pathWithin(canonicalRoot, canonicalPath) {
		return "", fmt.Errorf("path %q is outside request workspace", path)
	}
	return canonicalPath, nil
}

func evalSymlinksAllowMissing(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err == nil {
		return canonical, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	canonicalParent, parentErr := evalSymlinksAllowMissing(parent)
	if parentErr != nil {
		return "", parentErr
	}
	return filepath.Join(canonicalParent, filepath.Base(path)), nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

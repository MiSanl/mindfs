package usecase

import (
	"testing"

	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/session"
)

func newTestSessionManager(t *testing.T, root rootfs.RootInfo) *session.Manager {
	t.Helper()
	manager := session.NewManager(root)
	t.Cleanup(func() { _ = manager.Shutdown() })
	return manager
}

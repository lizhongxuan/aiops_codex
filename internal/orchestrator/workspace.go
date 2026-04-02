package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func MissionWorkspaceRoot(defaultWorkspace, missionID string) string {
	return filepath.Join(strings.TrimSpace(defaultWorkspace), "missions", strings.TrimSpace(missionID))
}

func PlannerWorkspacePath(defaultWorkspace, missionID string) string {
	return MissionWorkspaceRoot(defaultWorkspace, missionID)
}

func WorkerLocalWorkspacePath(defaultWorkspace, missionID, hostID string) string {
	return filepath.Join(MissionWorkspaceRoot(defaultWorkspace, missionID), "hosts", strings.TrimSpace(hostID))
}

func WorkerRemoteWorkspacePath(remoteRoot, missionID, hostID string) string {
	return filepath.Join(strings.TrimSpace(remoteRoot), strings.TrimSpace(missionID), strings.TrimSpace(hostID))
}

func EnsureWorkspace(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

func BootstrapRemoteWorkspaceCommand(remotePath string) string {
	return fmt.Sprintf("mkdir -p %s", strings.TrimSpace(remotePath))
}
